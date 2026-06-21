package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
	"github.com/anonde-io/anonde/internal/api"
	"github.com/anonde-io/anonde/internal/content"
	"github.com/anonde-io/anonde/internal/core"
	"github.com/anonde-io/anonde/internal/metrics"
	"github.com/anonde-io/anonde/internal/policy"
	"github.com/anonde-io/anonde/internal/store"
	"github.com/anonde-io/anonde/internal/telemetry"
)

func main() {
	addr := listenAddr()
	analyzerEngine, backendName, modelName := analyzerFromEnv()

	// Fail-closed NER verification before serving traffic; see
	// verifyNERBackendOrFail (and the ANONDE_ALLOW_NER_FALLBACK escape hatch).
	verifyNERBackendOrFail(analyzerEngine, backendName)

	// One-shot bootstrap used by Dockerfile.anonde-ner: initialise the
	// active analyzer, run one trivial inference call to force the NER
	// backend to download / cache its model into GLINER_MODELS_DIR, then
	// exit cleanly. The runtime image then ships with the model on disk
	// and never needs network access at startup.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DOWNLOAD_MODELS_ONLY")), "1") {
		downloadModelsAndExit(analyzerEngine)
	}

	// WARMUP_ON_START=1 runs one trivial Analyze synchronously before the
	// HTTP server starts listening. For NER backends this forces the
	// sync.Once-gated ONNX session creation to happen at boot; failure
	// (typically OOM under-provisioning) is then visible in machine boot
	// logs instead of as a stuck first user request, and the first real
	// request sees ~150 ms latency instead of 5–30 s. No-op overhead on
	// patterns-only backends.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WARMUP_ON_START")), "1") {
		warmupAnalyzer(analyzerEngine)
	}

	vaultTTL := durationFromEnv("ANONDE_VAULT_TTL", 0)
	storeTTL := durationFromEnv("ANONDE_STORE_TTL", 0)
	maxBytes := bytesFromEnv("MAX_CONTENT_BYTES", api.DefaultMaxRequestBytes)

	vault, anonStore, storeName, closeStore := selectStoreBackend(vaultTTL, storeTTL)
	defer closeStore()

	// Build the metrics surface once. The Recorder is wired into both
	// the analyzer and the core.Service, and the same private registry
	// backs the optional second listener below. Opt-out is total: with
	// METRICS_ENABLED=false the analyzer/Service see a no-op Recorder
	// and the second listener is suppressed regardless of METRICS_BIND.
	metricsEnabled := boolFromEnv("METRICS_ENABLED", true)
	var (
		metricsReg *prometheus.Registry
		recorder   metrics.Recorder
	)
	if metricsEnabled {
		metricsReg = prometheus.NewRegistry()
		recorder = metrics.New(metricsReg)
	} else {
		recorder = metrics.NewNoop()
	}
	// Telemetry wiring. Default-on, opt-out via ANONDE_TELEMETRY=off
	// or hard-off via ANONDE_OFFLINE=1. When enabled, the collector
	// is wrapped around the metrics Recorder so a single observation
	// in the Service path feeds both /metrics and the heartbeat
	// payload, with no duplication of measurement code. The static
	// half of the payload (install id, version, backend, build tag)
	// is resolved here so the Service layer never sees telemetry.
	telemetryCfg, telemetryCollector := buildTelemetryConfig(backendName)
	if telemetryCollector != nil {
		recorder = telemetry.WrapRecorder(recorder, telemetryCollector)
	}
	analyzerEngine.SetMetrics(recorder)

	svc := core.NewService(
		analyzerEngine,
		anonde.DefaultAnonymizerEngine(),
		vault,
		anonStore,
		&policy.Static{},
		recorder,
	)
	svc.SetVersionInfo(core.VersionInfo{
		AnalyzerBackend: backendName,
		Model:           modelName,
		BuildSHA:        buildSHA(),
		GoVersion:       runtime.Version(),
		APIVersion:      "v1",
	})

	// Register the scrape-time gauges collector now that vault / store
	// / analyzer are all known. Each callback is a thin adapter to keep
	// the metrics package free of internal/core and internal/store
	// dependencies (and vice versa). Wired only when metrics are on,
	// otherwise the registry doesn't exist.
	if metricsEnabled {
		metrics.RegisterGauges(metricsReg, metrics.GaugesConfig{
			Vault: func() metrics.Stats {
				s := vault.Stats()
				return metrics.Stats{Entries: s.Entries, Bytes: s.Bytes}
			},
			Store: func() metrics.Stats {
				s := anonStore.Stats()
				return metrics.Stats{Entries: s.Entries, Bytes: s.Bytes}
			},
			CustomRecognizers: func() int { return len(analyzerEngine.Registry.All()) },
			NEREnabled:        func() bool { return backendName != "patterns" && backendName != "" },
			Build: metrics.BuildInfo{
				Version:   buildSHA(),
				BuildTags: buildTagsLabel(),
				Backend:   backendName,
			},
		})
	}

	httpAPI := api.NewHTTPServer(svc)
	httpAPI.SetMaxRequestBytes(maxBytes)

	// ANONDE_MAX_CONCURRENT_REQUESTS gates total in-flight HTTP work.
	// Unset / 0 / negative = unlimited (current behaviour).
	concurrencyCap := concurrencyCapFromEnv()
	if concurrencyCap > 0 {
		log.Printf("concurrency budget: max %d in-flight requests", concurrencyCap)
	}

	// OpenAI-compatible proxy (POST /v1/chat/completions). Always
	// mounted; upstream defaults to OpenAI. The upstream provider is
	// chosen in-band by a "provider/model" prefix on the model field
	// (v0.1 supports "openai/" only). Point ANONDE_OPENAI_BASE_URL at
	// any OpenAI-compatible endpoint (e.g. a local Ollama) to retarget.
	openAIBase := strings.TrimSpace(os.Getenv("ANONDE_OPENAI_BASE_URL"))
	httpAPI.SetOpenAIProxy(api.OpenAIProxyConfig{
		UpstreamBaseURL: openAIBase,
		UpstreamAPIKey:  strings.TrimSpace(os.Getenv("ANONDE_OPENAI_API_KEY")),
		DefaultTenant:   strings.TrimSpace(os.Getenv("ANONDE_PROXY_TENANT")),
		RequestTimeout:  durationFromEnv("ANONDE_PROXY_TIMEOUT", 0),
	})
	if openAIBase == "" {
		openAIBase = "https://api.openai.com/v1 (default)"
	}
	log.Printf("openai-compatible proxy enabled at POST /v1/chat/completions (upstream=%s)", openAIBase)

	// PDF redaction endpoint (POST /v1/anonymizations/pdf). Opt-in
	// via ANONDE_PDF_ENABLED=1 because it requires pdftoppm + tesseract
	// on PATH for OCR on scanned PDFs and loads a ~500 MB YOLOS
	// signature-detection model into memory. Endpoint returns 501 with
	// a pointer at this env var when disabled.
	//
	// The YOLOS signature detector is always loaded when PDF is enabled
	// there's no per-request way to skip it, and the
	// ANONDE_PDF_VISION_MODEL split flag has been retired. Memory cost
	// (~500 MB resident) is the price of accepting PDFs; operators who
	// can't afford it should leave ANONDE_PDF_ENABLED unset and route
	// PDF traffic to a separate node.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ANONDE_PDF_ENABLED")), "1") {
		opts := content.RedactPDFOptions{
			Engine:          analyzerEngine,
			AnalysisCfg:     analyzer.AnalysisConfig{ScoreThreshold: 0.30, RemoveConflicts: true},
			DPI:             200,
			BoxPadding:      2,
			VisualHeuristic: true,
		}
		detector, derr := content.LoadSignatureDetector(strings.TrimSpace(os.Getenv("ANONDE_SIGNATURE_MODEL_PATH")))
		if derr != nil {
			log.Fatalf("ANONDE_PDF_ENABLED=1 but failed to load signature detector: %v", derr)
		}
		opts.VisualDetector = detector
		svc.SetPDFRedactor(core.NewPDFRedactor(opts))
		log.Printf("pdf endpoint enabled at POST /v1/anonymizations/pdf + GET /v1/anonymizations/{id}/reveal-pdf (signature detector loaded)")
	}

	// Wrap the routes in the concurrency limiter as the outermost layer
	// so health checks ARE gated too; that's intentional. An unhealthy
	// server should signal "busy" so load balancers route elsewhere.
	handler := newConcurrencyLimiter(concurrencyCap).wrap(httpAPI.Routes())

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		Protocols:         api.NewServerProtocols(),
	}

	// Optional second listener for /metrics + /healthz. We keep
	// Prometheus off the public :8081 surface so an operator can
	// expose 8081 publicly while leaving the metrics endpoint bound
	// to localhost (recommended: METRICS_BIND=127.0.0.1:9090) or to a
	// private network interface. METRICS_BIND empty → no second
	// listener, no surface change vs. pre-metrics anonde.
	metricsBind := strings.TrimSpace(os.Getenv("METRICS_BIND"))
	if metricsEnabled && metricsBind != "" {
		startMetricsListener(metricsBind, metricsReg)
	}

	// Start the telemetry sender after the HTTP listener is built but
	// before it blocks on ListenAndServe. The returned stop func is
	// kept for a future graceful-shutdown handler; today it just
	// runs on the fatal-error return path, which is correct enough
	// — the kernel kills the goroutine on a hard exit and the next
	// boot's last_heartbeat check makes that safe.
	stopTelemetry := telemetry.Start(context.Background(), telemetryCfg)
	defer stopTelemetry()

	log.Printf("anonde server listening on %s (max_request_bytes=%d backend=%s model=%s store=%s)",
		addr, maxBytes, backendName, modelName, storeName)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// buildTelemetryConfig resolves the telemetry env-var surface and
// returns the Config + Collector pair main needs. Returns a nil
// collector and a disabled config when telemetry is off
// (ANONDE_TELEMETRY=off or ANONDE_OFFLINE=1) so callers can do a
// single `if collector != nil` check.
//
// Env precedence: ANONDE_OFFLINE=1 wins outright (the umbrella
// "no outbound calls" knob; covers telemetry and any future remote
// model fetches). Otherwise ANONDE_TELEMETRY=off / 0 / false / no
// disables; anything else keeps the default-on behaviour the launch
// plan calls for.
//
// The startup log line is emitted here so it lands near the
// "anonde server listening" line, where operators look first.
func buildTelemetryConfig(backendName string) (telemetry.Config, *telemetry.Collector) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ANONDE_OFFLINE")), "1") {
		log.Printf("telemetry disabled (ANONDE_OFFLINE=1; all outbound calls suppressed)")
		return telemetry.Config{Enabled: false}, nil
	}
	if !boolFromEnv("ANONDE_TELEMETRY", true) {
		log.Printf("telemetry disabled (ANONDE_TELEMETRY=off)")
		return telemetry.Config{Enabled: false}, nil
	}

	id, dir, err := telemetry.LoadOrCreateInstallID()
	if err != nil {
		log.Printf("telemetry: install id unavailable (%v); disabling sender", err)
		return telemetry.Config{Enabled: false}, nil
	}
	if dir == "" {
		log.Printf("telemetry: install id is ephemeral; data dir not writable, heartbeat will run without cross-restart correlation")
	}

	endpoint := strings.TrimSpace(os.Getenv("ANONDE_TELEMETRY_URL"))
	if endpoint == "" {
		endpoint = telemetry.DefaultEndpoint
	}

	collector := telemetry.NewCollector()
	cfg := telemetry.Config{
		Enabled:   true,
		Endpoint:  endpoint,
		Collector: collector,
		DataDir:   dir,
		Static: telemetry.StaticInfo{
			InstallID: id,
			Version:   buildSHA(),
			BuildTag:  buildTagsLabel(),
			Backend:   backendName,
		},
	}
	// The disclosure line is verbatim from the launch plan; do not
	// soften it. Operators who care about telemetry read for the
	// opt-out instruction here.
	log.Printf("telemetry enabled (anonymous, 24h heartbeat). set ANONDE_TELEMETRY=off to disable.")
	return cfg, collector
}

// startMetricsListener spawns the second HTTP server in a goroutine.
// Only /metrics and /healthz are mounted; the public Connect / REST
// / gRPC routes stay on the primary listener. A non-fatal error on
// boot is logged but does not kill the main process: metrics being
// unavailable should not take the data plane down.
//
// Operators should bind to 127.0.0.1:9090 by default and let their
// network policy (or Prometheus scrape config) reach in over the
// loopback. Binding to :9090 exposes metrics on every interface and
// is acceptable only on an already-firewalled host.
func startMetricsListener(bind string, reg *prometheus.Registry) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		// Default registry is intentionally NOT used. EnableOpenMetrics
		// gives us exemplars-capable exposition (Prometheus 2.43+
		// accepts it transparently).
		EnableOpenMetrics: true,
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              bind,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("metrics listener on %s (routes: /metrics, /healthz)", bind)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics listener stopped: %v", err)
		}
	}()
}

// buildTagsLabel reports which optional build tag set the binary was
// compiled with. Stamped onto anonde_build_info so dashboards can
// distinguish a patterns-only image (default build) from an NER
// image (-tags ner). Set by build_tags_*.go.
var buildTagsLabel = func() string { return "default" }

// boolFromEnv parses a yes/no/true/false/1/0-style env var with a
// fallback. Unrecognised values are logged-and-fall-back rather than
// fatal so a typo in a deploy doesn't gate startup.
func boolFromEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch raw {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("ignoring %s=%q (use true|false); defaulting to %v", key, raw, fallback)
		return fallback
	}
}

// buildSHA pulls the vcs.revision stamp the Go toolchain bakes into
// the binary at build time. Returns "" when the build was made outside
// a git checkout (e.g. go run from a tarball).
func buildSHA() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// verifyNERBackendOrFail is the boot-time fail-closed guard for the
// silent-fallback bug class: when an NER backend was explicitly selected
// (ANALYZER_BACKEND=gliner|gliner-flat|gliner-stack) but the model
// can't load, we exit non-zero rather than serve patterns-only while reporting
// backend=gliner and leaking PERSON/ORG/LOCATION with no error surfaced. No-op
// for the patterns backend, so that path and per-request disable_ner are
// unaffected. ANONDE_ALLOW_NER_FALLBACK=1 is a deliberate escape hatch that
// downgrades the hard exit to a loud ERROR log and boots patterns-only; OFF by
// default because fail-closed is the safe default for a redaction tool.
func verifyNERBackendOrFail(engine *analyzer.AnalyzerEngine, backendName string) {
	if backendName == "" || backendName == "patterns" {
		return
	}
	if !analyzer.HasNERRecognizer(engine) {
		// Usual cause: a default-build binary (no -tags ner) running with
		// ANALYZER_BACKEND=gliner.
		msg := fmt.Sprintf("ANALYZER_BACKEND=%s requested but no NER recognizer is registered "+
			"(is this binary built with -tags ner?); refusing to serve patterns-only under a NER backend label", backendName)
		if boolFromEnv("ANONDE_ALLOW_NER_FALLBACK", false) {
			log.Printf("ERROR: %s; ANONDE_ALLOW_NER_FALLBACK=1 set, degrading to patterns-only", msg)
			return
		}
		log.Fatalf("%s (set ANONDE_ALLOW_NER_FALLBACK=1 to deliberately degrade to patterns-only)", msg)
	}

	timeout := durationFromEnv("ANONDE_NER_VERIFY_TIMEOUT", 5*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	log.Printf("verifying NER backend %q loads (timeout=%s) ...", backendName, timeout)
	start := time.Now()
	if err := analyzer.VerifyNERBackend(ctx, engine); err != nil {
		if boolFromEnv("ANONDE_ALLOW_NER_FALLBACK", false) {
			log.Printf("ERROR: NER backend %q failed to load after %s: %v; "+
				"ANONDE_ALLOW_NER_FALLBACK=1 set, degrading to patterns-only (PII recall reduced, leaks possible)",
				backendName, time.Since(start), err)
			return
		}
		log.Fatalf("NER backend %q failed to load after %s: %v "+
			"(refusing to start and silently fall back to patterns-only; "+
			"set ANONDE_ALLOW_NER_FALLBACK=1 to deliberately degrade instead)",
			backendName, time.Since(start), err)
	}
	log.Printf("NER backend %q verified in %s", backendName, time.Since(start))
}

// warmupAnalyzer forces the analyzer engine's lazy initialisation paths
// (notably the GLiNER ONNX session under sync.Once) to run before the HTTP
// server starts listening. On failure the process exits with the analyzer
// error, surfacing under-provisioning (OOM) at boot rather than as a hung
// first user request.
//
// The 5 minute timeout accommodates the worst-case cold disk read +
// pure-Go ONNX session init on the smallest Fly machine. Tighten via
// ANONDE_WARMUP_TIMEOUT if you have a tighter SLA for boot latency.
func warmupAnalyzer(engine *analyzer.AnalyzerEngine) {
	timeout := durationFromEnv("ANONDE_WARMUP_TIMEOUT", 5*time.Minute)
	log.Printf("WARMUP_ON_START=1: priming analyzer (timeout=%s)", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	_, err := engine.Analyze(ctx, "John Smith works at Mercy Hospital.", analyzer.AnalysisConfig{
		Language:       "en",
		ScoreThreshold: 0.3,
	})
	if err != nil {
		log.Fatalf("analyzer warmup failed after %s: %v", time.Since(start), err)
	}
	log.Printf("analyzer warmup complete in %s", time.Since(start))

	// Pool pre-warm: the single Analyze above only initialised the first
	// pool instance. Walk the registry and call Warmup on every pool so
	// every instance pays the model-load cost concurrently NOW instead
	// of staggered across the first N user requests. The single Analyze
	// runs FIRST so any model-file download (DOWNLOAD_MODELS_ONLY-style
	// cold cache) happens once before the N parallel calls hammer the
	// same model file on disk.
	for _, rec := range engine.Registry.All() {
		switch p := rec.(type) {
		case *recognizers.GLiNERPool:
			log.Printf("warmup: pre-warming %T (size=%d) ...", p, p.Size())
			poolStart := time.Now()
			if err := p.Warmup(ctx); err != nil {
				log.Printf("warmup: %T failed after %s: %v (server continues; instances retry on first request)",
					p, time.Since(poolStart), err)
			} else {
				log.Printf("warmup: %T ready in %s", p, time.Since(poolStart))
			}
		case *recognizers.GLiNERFlatPool:
			log.Printf("warmup: pre-warming %T (size=%d) ...", p, p.Size())
			poolStart := time.Now()
			if err := p.Warmup(ctx); err != nil {
				log.Printf("warmup: %T failed after %s: %v (server continues; instances retry on first request)",
					p, time.Since(poolStart), err)
			} else {
				log.Printf("warmup: %T ready in %s", p, time.Since(poolStart))
			}
		}
	}
}

// concurrencyLimiter is a tiny semaphore-style HTTP middleware. Acquire
// is non-blocking; when the semaphore is full the (N+1)th request is
// rejected with 429 immediately instead of queueing. Queueing would
// just convert a throughput problem into a latency problem; backpressure
// to the caller is the correct shape for a self-hosted server with no
// idea what the operator's downstream SLO is.
//
// `cap <= 0` is the unlimited mode; wrap() returns the handler
// unchanged so there's zero overhead on the default-unset path.
type concurrencyLimiter struct {
	sem chan struct{}
}

// newConcurrencyLimiter builds a limiter from the configured cap. A cap
// of 0 or less returns a zero-value limiter (sem is nil) and wrap()
// becomes a no-op pass-through.
func newConcurrencyLimiter(cap int) *concurrencyLimiter {
	if cap <= 0 {
		return &concurrencyLimiter{}
	}
	return &concurrencyLimiter{sem: make(chan struct{}, cap)}
}

// wrap returns an http.Handler that gates `next` behind the semaphore
// when one is configured. The unlimited case skips the middleware
// entirely; identical to the pre-change behaviour.
//
// Panic discipline: the semaphore is correctly released even if the
// downstream handler panics (Go runs deferred funcs during unwind),
// but a bare panic would otherwise propagate up through net/http and
// drop the connection without a body. We recover here so the process
// stays alive AND the client sees a 500; wrapped in a nested
// defer-recover because writing to a response writer whose headers
// are already committed is itself an error path we don't want to
// re-panic from.
func (l *concurrencyLimiter) wrap(next http.Handler) http.Handler {
	if l == nil || l.sem == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case l.sem <- struct{}{}:
			defer func() { <-l.sem }()
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("panic in handler %s %s: %v", r.Method, r.URL.Path, rec)
					// Best-effort 500. If the writer is already
					// committed (headers + body sent) WriteHeader
					// silently no-ops; if Write itself panics
					// because the connection is gone, the nested
					// recover swallows it so we still exit cleanly.
					func() {
						defer func() { _ = recover() }()
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = w.Write([]byte(`{"error":"internal server error"}`))
					}()
				}
			}()
			next.ServeHTTP(w, r)
		default:
			// Semaphore full. Reject immediately with Retry-After.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"max concurrent requests reached, retry"}`))
		}
	})
}

// concurrencyCapFromEnv parses ANONDE_MAX_CONCURRENT_REQUESTS. Unset /
// 0 / negative returns 0 ("unlimited", current behaviour); malformed
// values are logged and treated as unset. Matches the GLINER_POOL_SIZE
// precedent; a typo never blocks boot.
func concurrencyCapFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("ANONDE_MAX_CONCURRENT_REQUESTS"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("ANONDE_MAX_CONCURRENT_REQUESTS=%q ignored: %v (no concurrency cap)", raw, err)
		return 0
	}
	if n < 1 {
		return 0
	}
	return n
}

// downloadModelsAndExit triggers a single inference call so the configured
// backend's underlying NER model is fetched into its cache directory, then
// exits. Used at Docker build time (see Dockerfile.anonde-ner) to bake
// the model into the runtime image; the runtime then has no cold-start
// download and needs no outbound network access.
//
// Timeout is generous (10 minutes) because the first call also has to
// download the model files from HuggingFace Hub on slow links.
func downloadModelsAndExit(engine *analyzer.AnalyzerEngine) {
	log.Println("DOWNLOAD_MODELS_ONLY=1: warming up backend so model artifacts are cached, then exiting")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, err := engine.Analyze(ctx, "John Smith works at Mercy Hospital.", analyzer.AnalysisConfig{
		Language:       "en",
		ScoreThreshold: 0.3,
	})
	if err != nil {
		log.Fatalf("model warmup failed: %v", err)
	}
	log.Println("models cached successfully")
	os.Exit(0)
}

func listenAddr() string {
	if addr := strings.TrimSpace(os.ExpandEnv(os.Getenv("ANONDE_ADDR"))); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return ":" + port
	}
	return ":8080"
}

// analyzerFromEnv selects the NER backend.
//
// Default: patterns (no NER, no model download, no external service).
// This is the fast / safe default and what Dockerfile.anonde ships. The
// German recognizer kernel covers most clinical PII without NER.
//
// Opt-ins:
//
//   - ANALYZER_BACKEND=gliner; in-process GLiNER PII via yalue/onnxruntime_go.
//     Open-set NER with strong German clinical recall
//     (see bench/corpora/openmed/REPORT_FINAL.md). Requires `-tags ner` AND
//     CGO_ENABLED=1 AND libonnxruntime.so reachable at runtime.
//   - ANALYZER_BACKEND=gliner-flat; single GLiNER recognizer with the
//     flat / token decoder (knowledgator/gliner-pii-large-v1.0 and other
//     4-input BIO ONNX exports). Same build / CGO / libonnxruntime
//     requirements as gliner (`-tags ner`). Same env knobs (GLINER_MODEL,
//     GLINER_THRESHOLD, GLINER_MODELS_DIR, ORT_SO_PATH).
//   - ANALYZER_BACKEND=gliner-stack; registers BOTH the span-decoder
//     base recognizer AND a flat-decoder recognizer in the same engine
//     so each doc is scored by both models and the conflict resolver
//     unions their findings. Configured by the existing GLINER_* env
//     vars (BASE slot) plus the ANONDE_GLINER_FLAT_* env vars (flat slot,
//     defaults to knowledgator/gliner-pii-large-v1.0 / model.onnx).
//     ~2x latency vs gliner alone; pairs with Dockerfile.anonde-ner-stack
//     which bakes both ONNX exports at build time. This is the lowest-
//     leak deployment shape (local bench Σ ALL ≈ 9.7% across 30 corpora
//     vs ~12.9% for gliner alone).
//
// Presidio is no longer a runtime backend. To benchmark anonde against
// Presidio, see bench/corpora/ai4privacy_en/.
//
// Pool sizing (gliner / gliner-flat / gliner-stack):
//
//   - GLINER_POOL_SIZE; integer ≥ 2 builds an N-instance pool for the
//     base GLiNER recognizer (span decoder for `gliner` / `gliner-stack`,
//     flat decoder for `gliner-flat`). Unset / 0 / 1 → single recognizer
//     (current behaviour, no change).
//   - ANONDE_GLINER_FLAT_POOL_SIZE; integer ≥ 2 builds an N-instance
//     pool for the FLAT recognizer of `gliner-stack` only. Unset / 0 / 1
//     → single flat recognizer alongside the base pool.
//
// Memory cost is the binding constraint: ~500 MB per BASE quint8
// instance, ~1.4 GB per LARGE FP32 instance. Size pools against your
// VM's RAM, not your CPU count. In a `gliner-stack` deployment the
// LARGE flat pool should usually be smaller than the BASE pool; e.g.
// GLINER_POOL_SIZE=4 + ANONDE_GLINER_FLAT_POOL_SIZE=2 peaks ~4.8 GB.
//
// ONNX Runtime session tuning (all GLiNER backends):
//
//   - ANONDE_ORT_INTRA_OP_THREADS; integer ≥ 1. Number of threads
//     ORT uses INSIDE one op (e.g. a matmul). Unset → ORT default
//     (num cores). Lower this when stacking pools; e.g. a 4-instance
//     pool with intra=4 contends for all CPUs; intra=2 may improve
//     total throughput by reducing thread-pool oversubscription.
//   - ANONDE_ORT_INTER_OP_THREADS; integer ≥ 1. Number of threads ORT
//     uses to run independent ops in parallel. Unset → ORT default
//     (1). Rarely worth raising on transformer graphs.
//   - ANONDE_ORT_GRAPH_OPT_LEVEL; "disabled" | "basic" | "extended"
//     | "all". Unset → ORT default ("basic"). "all" can shave a few
//     percent off latency at the cost of longer session-open time
//     (which the warmup absorbs).
func analyzerFromEnv() (*analyzer.AnalyzerEngine, string, string) {
	backend := strings.ToLower(strings.TrimSpace(getenvDefault("ANALYZER_BACKEND", "patterns")))
	switch backend {
	case "patterns", "patterns-only", "":
		log.Printf("analyzer backend: patterns-only (no NER)")
		return anonde.DefaultAnalyzerEngine(), "patterns", ""
	case "gliner":
		modelName := getenvDefault("GLINER_MODEL", "onnx-community/gliner_multi_pii-v1")
		onnxPath := glinerOnnxFileFromEnv(modelName)
		threshold := glinerThresholdFromEnv()
		// Multi-model GLiNER stacking: if ANONDE_NER_STACK is set,
		// build an ensemble across the listed model IDs instead of the
		// single-model path. Unset → fall through to current behaviour
		// with no change. EnsembleFromEnv returns (nil, nil) on unset
		// and (nil, error) on a malformed value (e.g. ",,,") so a typo
		// fails fast at boot rather than silently disabling NER.
		if ens, ensErr := recognizers.EnsembleFromEnv(threshold, os.Getenv("ORT_SO_PATH"), glinerSpanFilterFromEnv()); ensErr != nil {
			log.Fatalf("ANONDE_NER_STACK: %v", ensErr)
		} else if ens != nil {
			log.Printf("analyzer backend: gliner-ensemble (threshold=%.2f); single-model GLINER_MODEL=%s ignored",
				threshold, modelName)
			return anonde.DefaultAnalyzerEngineWithGLiNEREnsemble(ens), "gliner-ensemble", os.Getenv("ANONDE_NER_STACK")
		}
		labels, labelToEntity := glinerLabelSetFromEnv()
		cfg := recognizers.GLiNERConfig{
			ModelsDir:         os.Getenv("GLINER_MODELS_DIR"),
			ModelName:         modelName,
			OnnxFilePath:      onnxPath,
			AutoDownload:      true,
			SharedLibraryPath: os.Getenv("ORT_SO_PATH"),
			Threshold:         threshold,
			ClassThresholds:   glinerClassThresholdsFromEnv(),
			SpanFilter:        glinerSpanFilterFromEnv(),
			// nil → library chat default; GLINER_LABEL_SET pins clinical/finance/legal.
			Labels:        labels,
			LabelToEntity: labelToEntity,
		}
		if poolSize := glinerPoolSizeFromEnv("GLINER_POOL_SIZE"); poolSize >= 2 {
			pool, err := recognizers.NewGLiNERPool(cfg, poolSize)
			if err != nil {
				log.Fatalf("gliner pool init (size=%d): %v", poolSize, err)
			}
			log.Printf("analyzer backend: gliner pool (size=%d, model=%s, onnx=%s, threshold=%.2f)", poolSize, modelName, onnxPath, threshold)
			return anonde.DefaultAnalyzerEngineWithGLiNERPool(pool), "gliner", modelName
		}
		log.Printf("analyzer backend: gliner (model=%s, onnx=%s, threshold=%.2f)", modelName, onnxPath, threshold)
		return anonde.DefaultAnalyzerEngineWithGLiNERConfig(cfg), "gliner", modelName
	case "gliner-flat":
		// Single flat / token-decoder GLiNER (4-input BIO ONNX export).
		// Same knobs as `gliner`; GLINER_MODEL, GLINER_THRESHOLD,
		// GLINER_MODELS_DIR, ORT_SO_PATH. Defaults aimed at the LARGE PII
		// model since that's the canonical flat-decoder export today.
		modelName := getenvDefault("GLINER_MODEL", "knowledgator/gliner-pii-large-v1.0")
		onnxPath := glinerOnnxFileFromEnv(modelName)
		threshold := glinerThresholdFromEnv()
		labels, labelToEntity := glinerLabelSetFromEnv()
		cfg := recognizers.GLiNERConfig{
			ModelsDir:         os.Getenv("GLINER_MODELS_DIR"),
			ModelName:         modelName,
			OnnxFilePath:      onnxPath,
			AutoDownload:      true,
			SharedLibraryPath: os.Getenv("ORT_SO_PATH"),
			Threshold:         threshold,
			ClassThresholds:   glinerClassThresholdsFromEnv(),
			SpanFilter:        glinerSpanFilterFromEnv(),
			// nil → library chat default; GLINER_LABEL_SET pins clinical/finance/legal.
			Labels:        labels,
			LabelToEntity: labelToEntity,
		}
		if poolSize := glinerPoolSizeFromEnv("GLINER_POOL_SIZE"); poolSize >= 2 {
			pool, err := recognizers.NewGLiNERFlatPool(cfg, poolSize)
			if err != nil {
				log.Fatalf("gliner-flat pool init (size=%d): %v", poolSize, err)
			}
			log.Printf("analyzer backend: gliner-flat pool (size=%d, model=%s, onnx=%s, threshold=%.2f)", poolSize, modelName, onnxPath, threshold)
			return anonde.DefaultAnalyzerEngineWithGLiNERFlatPool(pool), "gliner-flat", modelName
		}
		log.Printf("analyzer backend: gliner-flat (model=%s, onnx=%s, threshold=%.2f)", modelName, onnxPath, threshold)
		return anonde.DefaultAnalyzerEngineWithGLiNERFlatConfig(cfg), "gliner-flat", modelName
	case "gliner-stack":
		// Span-decoder BASE + flat-decoder FLAT in one engine. The local
		// 30-corpus bench measured Σ ALL ≈ 9.7% with this shape (vs
		// ~12.9% for `gliner` alone). Two ONNX sessions resident; ~2x
		// the per-request inference latency. Pair with
		// Dockerfile.anonde-ner-stack which bakes both ONNX exports.
		baseModel := getenvDefault("GLINER_MODEL", "knowledgator/gliner-pii-base-v1.0")
		baseOnnx := glinerOnnxFileFromEnv(baseModel)
		baseThreshold := glinerThresholdFromEnv()
		flatModel := getenvDefault("ANONDE_GLINER_FLAT_MODEL", "knowledgator/gliner-pii-large-v1.0")
		// LARGE export ships its FP32 ONNX at onnx/model.onnx (verified on HF:
		// knowledgator/gliner-pii-large-v1.0 has no repo-root model.onnx — the
		// old "model.onnx" default 404'd at download time, silently shipping a
		// base-only stack image; see ANO-9). ANONDE_GLINER_FLAT_ONNX_FILE
		// overrides for other flat exports that lay out their ONNX differently.
		flatOnnx := getenvDefault("ANONDE_GLINER_FLAT_ONNX_FILE", "onnx/model.onnx")
		flatThreshold := baseThreshold
		if raw := strings.TrimSpace(os.Getenv("ANONDE_GLINER_FLAT_THRESHOLD")); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				flatThreshold = v
			} else {
				log.Printf("ANONDE_GLINER_FLAT_THRESHOLD=%q ignored: %v", raw, err)
			}
		}
		basePoolSize := glinerPoolSizeFromEnv("GLINER_POOL_SIZE")
		flatPoolSize := glinerPoolSizeFromEnv("ANONDE_GLINER_FLAT_POOL_SIZE")
		log.Printf("analyzer backend: gliner-stack (base=%s onnx=%s thr=%.2f base_pool=%d) + flat (model=%s onnx=%s thr=%.2f flat_pool=%d)",
			baseModel, baseOnnx, baseThreshold, basePoolSize,
			flatModel, flatOnnx, flatThreshold, flatPoolSize)
		labels, labelToEntity := glinerLabelSetFromEnv()
		stackSpanFilter := glinerSpanFilterFromEnv()
		stackClassThresholds := glinerClassThresholdsFromEnv()
		baseCfg := recognizers.GLiNERConfig{
			ModelsDir:         os.Getenv("GLINER_MODELS_DIR"),
			ModelName:         baseModel,
			OnnxFilePath:      baseOnnx,
			AutoDownload:      true,
			SharedLibraryPath: os.Getenv("ORT_SO_PATH"),
			Threshold:         baseThreshold,
			ClassThresholds:   stackClassThresholds,
			SpanFilter:        stackSpanFilter,
			Labels:            labels,
			LabelToEntity:     labelToEntity,
		}
		flatCfg := recognizers.GLiNERConfig{
			ModelsDir:         os.Getenv("GLINER_MODELS_DIR"),
			ModelName:         flatModel,
			OnnxFilePath:      flatOnnx,
			AutoDownload:      true,
			SharedLibraryPath: os.Getenv("ORT_SO_PATH"),
			Threshold:         flatThreshold,
			ClassThresholds:   stackClassThresholds,
			SpanFilter:        stackSpanFilter,
			Labels:            labels,
			LabelToEntity:     labelToEntity,
		}
		// Build the base slot: pool when sized, single recognizer
		// otherwise. The two helpers register the chosen NER in the same
		// pattern-recognizer registry shape, so the engine downstream is
		// indistinguishable from the single-recognizer path.
		var engine *analyzer.AnalyzerEngine
		if basePoolSize >= 2 {
			basePool, err := recognizers.NewGLiNERPool(baseCfg, basePoolSize)
			if err != nil {
				log.Fatalf("gliner-stack base pool init (size=%d): %v", basePoolSize, err)
			}
			engine = anonde.DefaultAnalyzerEngineWithGLiNERPool(basePool)
		} else {
			engine = anonde.DefaultAnalyzerEngineWithGLiNERConfig(baseCfg)
		}
		// Register the flat slot alongside the base. Same registry
		// dispatches to both per doc; analyzer.RemoveConflicts merges
		// overlaps via the NER-preferred rule for PERSON/ORG/LOC/AGE/
		// PROFESSION/NRP. LARGE FP32 is ~1.4 GB per instance, so the
		// flat pool defaults to a single recognizer when
		// ANONDE_GLINER_FLAT_POOL_SIZE is unset.
		if flatPoolSize >= 2 {
			flatPool, err := recognizers.NewGLiNERFlatPool(flatCfg, flatPoolSize)
			if err != nil {
				log.Fatalf("gliner-stack flat pool init (size=%d): %v", flatPoolSize, err)
			}
			engine.Registry.Add(flatPool)
		} else {
			engine.Registry.Add(recognizers.NewGLiNERFlatRecognizer(flatCfg))
		}
		return engine, "gliner-stack", baseModel + "+" + flatModel
	default:
		log.Fatalf("unsupported ANALYZER_BACKEND=%q (valid: patterns, gliner, gliner-flat, gliner-stack)", backend)
		return nil, "", ""
	}
}

// glinerPoolSizeFromEnv parses an integer pool size from the named env
// var. Returns 1 (single recognizer, no pool) when unset, malformed,
// or ≤ 0. A malformed value is logged but does NOT log.Fatalf,
// matching the GLINER_THRESHOLD precedent so a typo doesn't keep the
// server from booting; the operator gets the warning in startup logs
// and falls through to single-recognizer behaviour. Used by both
// GLINER_POOL_SIZE (BASE slot) and ANONDE_GLINER_FLAT_POOL_SIZE
// (FLAT slot of gliner-stack).
func glinerPoolSizeFromEnv(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("%s=%q ignored: %v (falling back to single recognizer)", key, raw, err)
		return 1
	}
	if n <= 0 {
		return 1
	}
	return n
}

// glinerThresholdFromEnv parses GLINER_THRESHOLD into a float. Zero means
// "use the recognizer's compiled-in default" (currently 0.40). Threshold
// is the highest-impact GLiNER knob; multilingual variants typically
// need ~0.25, the English base ~0.40. Override without rebuilding via
// the env var.
func glinerThresholdFromEnv() float64 {
	raw := strings.TrimSpace(os.Getenv("GLINER_THRESHOLD"))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		log.Printf("GLINER_THRESHOLD=%q ignored: %v", raw, err)
		return 0
	}
	return v
}

// glinerClassThresholdsFromEnv builds the GLiNER per-class threshold
// override map from env (GLINER_{PERSON,ORG,LOCATION,NRP}_THRESHOLD). Each
// value is used DIRECTLY (not min()'d), so an operator can RAISE a noisy
// fuzzy class above its recall-tuned floor to cut FPs. GLINER_STRICT=1
// applies the bench-picked STRICT floors (PERSON 0.50, ORG/LOC/NRP 0.55)
// for any class without an explicit override. Unset (and no STRICT) → nil;
// malformed values are logged and ignored (fails open).
func glinerClassThresholdsFromEnv() map[string]float64 {
	strict := boolFromEnv("GLINER_STRICT", false)
	out := map[string]float64{}
	type knob struct {
		env, canonical string
		strictFloor    float64
	}
	for _, k := range []knob{
		{"GLINER_PERSON_THRESHOLD", "PERSON", 0.50},
		{"GLINER_ORG_THRESHOLD", "ORGANIZATION", 0.55},
		{"GLINER_LOCATION_THRESHOLD", "LOCATION", 0.55},
		{"GLINER_NRP_THRESHOLD", "NRP", 0.55},
	} {
		if raw := strings.TrimSpace(os.Getenv(k.env)); raw != "" {
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil || v <= 0 {
				log.Printf("%s=%q ignored: %v", k.env, raw, err)
			} else {
				out[k.canonical] = v
				continue
			}
		}
		if strict {
			out[k.canonical] = k.strictFloor
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// glinerSpanFilterFromEnv builds the span-shape filter config from env.
// GLINER_STRICT=1 or GLINER_SPAN_FILTER=1 enables it with the default
// stoplist; GLINER_STOPLIST=a,b,c appends extra lower-cased denylist terms.
// Returns a zero (disabled) config when neither switch is set, so the
// default deploy is byte-for-byte unchanged.
func glinerSpanFilterFromEnv() recognizers.SpanFilterConfig {
	if !boolFromEnv("GLINER_STRICT", false) && !boolFromEnv("GLINER_SPAN_FILTER", false) {
		return recognizers.SpanFilterConfig{}
	}
	var extra []string
	if raw := strings.TrimSpace(os.Getenv("GLINER_STOPLIST")); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				extra = append(extra, t)
			}
		}
	}
	sf := recognizers.StrictSpanFilter(extra...)
	log.Printf("gliner: STRICT span-shape filter enabled (stoplist=%d terms; rejects UUID/locale/semver/model-slug/hex/base64/SCREAMING_SNAKE on fuzzy types)", len(sf.Stoplist))
	return sf
}

// glinerLabelSetFromEnv selects the GLiNER label list + its label→entity map
// from GLINER_LABEL_SET. chat is the default (unset/empty/default/unrecognised
// → nil,nil, which the library's empty-Labels fallback resolves to the chat
// DefaultPIILabels); clinical|finance|legal pin the matching
// recognizers.*PIILabels set. Mirrors the bench runner's resolveLabelSet.
func glinerLabelSetFromEnv() ([]string, map[string]string) {
	switch set := strings.ToLower(strings.TrimSpace(os.Getenv("GLINER_LABEL_SET"))); set {
	case "", "chat", "default":
		return nil, nil
	case "clinical":
		log.Printf("gliner label set: clinical (ClinicalPIILabels; AGE/PROFESSION/DATE + clinical/German-insurance labels)")
		return recognizers.ClinicalPIILabels, recognizers.ClinicalLabelToEntity
	case "finance":
		log.Printf("gliner label set: finance (FinancePIILabels; bank/routing/IBAN/SWIFT, card+CVV, tax IDs, account/transaction IDs)")
		return recognizers.FinancePIILabels, recognizers.FinancePIILabelToEntity
	case "legal":
		log.Printf("gliner label set: legal (LegalPIILabels; identity+geography, DATE/DOB kept, case/docket/matter/contract/bar IDs, court, parties)")
		return recognizers.LegalPIILabels, recognizers.LegalPIILabelToEntity
	default:
		log.Printf("GLINER_LABEL_SET=%q not recognised (valid: chat, clinical, finance, legal); defaulting to chat", set)
		return nil, nil
	}
}

// glinerOnnxFileFromEnv resolves which ONNX export of the GLiNER model
// to load. The GLiNER HF repos ship multiple quantization variants of
// the same weights; the choice is a recall / size / latency trade-off.
//
// Two env vars, in precedence order:
//
//   - GLINER_ONNX_FILE; explicit in-repo path (e.g. "onnx/model.onnx").
//     The escape hatch: use it for any repo whose file layout doesn't
//     match the convenience names below.
//   - GLINER_QUANT; convenience selector: "int8" | "fp16" | "fp32".
//     Maps to the conventional filenames in the knowledgator and
//     onnx-community GLiNER repos. FP32 is the default; production
//     ships full precision because the bench matrix proved INT8
//     uniformly depresses recall (Σ ALL leak 20.7% FP32 vs 26.6% INT8).
//
// Recall context: INT8 quantization uniformly depresses GLiNER's span
// logits (~0.18 in sigmoid space vs the FP32 weights). On clean
// saturated text this is invisible, but on lower-confidence multilingual
// legal / clinical corpora it pushes a band of true spans below the
// 0.40 threshold and leaks PII. Memory-constrained deployments that
// need to keep the image small (~530 MB instead of ~770 MB) can opt
// back into INT8 with GLINER_QUANT=int8 and accept the recall cost.
// See bench/probes/fp32_vs_int8/REPORT.md.
func glinerOnnxFileFromEnv(modelName string) string {
	// Explicit path wins outright.
	if raw := strings.TrimSpace(os.Getenv("GLINER_ONNX_FILE")); raw != "" {
		return raw
	}

	// onnx-community repos name the int8 build "model_quantized.onnx";
	// the knowledgator pii-base repo names it "model_quint8.onnx".
	int8Name := "onnx/model_quantized.onnx"
	if strings.Contains(strings.ToLower(modelName), "knowledgator") {
		int8Name = "onnx/model_quint8.onnx"
	}

	quant := strings.ToLower(strings.TrimSpace(os.Getenv("GLINER_QUANT")))
	switch quant {
	case "", "fp32", "float32", "full":
		// FP32 is the production default: the matrix proved INT8
		// uniformly depresses recall vs full precision (Σ ALL leak
		// 20.7% FP32 vs 26.6% INT8 across 30 corpora × 5 languages).
		// The image grows from ~530 MB to ~770 MB; small price for
		// 6pp better leak rate.
		return "onnx/model.onnx"
	case "int8", "quint8", "quantized":
		// Opt-in for memory-constrained deployments: the INT8 build
		// is ~196 MB on disk vs ~370 MB for FP32, but leaks more PII
		// on multilingual legal / clinical text. Use only when image
		// size is the binding constraint.
		return int8Name
	case "fp16", "float16":
		// CAVEAT: knowledgator/gliner-pii-base-v1.0's shipped
		// model_fp16.onnx currently has an onnxruntime-incompatible
		// graph; onnxruntime rejects it on session create with a
		// "Type Error: Type (tensor(float16)) of output arg ... Cast
		// ..." failure, and the GLiNER recognizer then silently falls
		// back to patterns-only. The mapping is kept (a future fixed
		// FP16 export from the upstream repo may load cleanly), but as
		// of now GLINER_QUANT=fp16 does NOT work for this model; use
		// fp32 (the default) for the full-precision build.
		return "onnx/model_fp16.onnx"
	default:
		log.Printf("GLINER_QUANT=%q not recognised (valid: fp32, int8, fp16); defaulting to fp32", quant)
		return "onnx/model.onnx"
	}
}

// selectStoreBackend picks the Vault + Store implementations based on
// STORE_BACKEND. Default is "memory" so existing dev / library users
// see no behaviour change.
//
// "bbolt" path resolution, in precedence order:
//
//  1. ANONDE_DATA_DIR/anonde.db — the anchor; same env var the
//     telemetry install_id resolver honours, so a single mounted
//     volume holds both files with a flat layout. This is what the
//     shipped Docker images use.
//  2. "anonde.db" in the process CWD — the bare-binary / library
//     fallback when no anchor is set.
//
// At-rest encryption is opt-in: set ANONDE_VAULT_KEY to a base64
// 32-byte key to enable AES-256-GCM, otherwise vault values are
// stored as plaintext JSON. We log loudly when encryption is off so
// an operator can't miss that the file holds raw PII.
//
// Returns the impls plus a `close` that the caller defers; this owns
// the bbolt *DB lifecycle when it's in play. The two adapters share
// the same DB file, so the close function flushes both buckets.
func selectStoreBackend(vaultTTL, storeTTL time.Duration) (core.Vault, core.Store, string, func()) {
	backend := strings.ToLower(strings.TrimSpace(getenvDefault("STORE_BACKEND", "memory")))
	switch backend {
	case "memory":
		return store.NewMemoryVaultWithTTL(vaultTTL),
			store.NewMemoryStoreWithTTL(storeTTL),
			"memory",
			func() {}
	case "bbolt":
		path := resolveBoltPath()
		var key []byte
		if strings.TrimSpace(os.Getenv("ANONDE_VAULT_KEY")) != "" {
			var err error
			key, err = store.LoadVaultKey()
			if err != nil {
				log.Fatalf("STORE_BACKEND=bbolt: %v", err)
			}
		} else {
			log.Printf("STORE_BACKEND=bbolt: ANONDE_VAULT_KEY not set; vault stored UNENCRYPTED at %s", path)
		}
		db, err := store.OpenDB(path)
		if err != nil {
			log.Fatalf("open bbolt db: %v", err)
		}
		boltVault, err := store.NewBoltVault(db, vaultTTL, key)
		if err != nil {
			_ = db.Close()
			log.Fatalf("init bolt vault: %v", err)
		}
		anonStore := store.NewBoltStore(db, storeTTL)

		// LRU cache in front of the vault to absorb the N-token-per-
		// reveal read amplification. Default 10k entries (~2 MB of
		// VaultEntry structs); ANONDE_VAULT_CACHE_SIZE=0 disables the
		// wrapper entirely and serves all reads through bbolt.
		cacheSize := intFromEnv("ANONDE_VAULT_CACHE_SIZE", 10_000)
		vault := store.NewCachedVault(boltVault, cacheSize)
		label := "bbolt:" + path
		if key == nil {
			label += " (plaintext)"
		} else {
			label += " (aes256gcm)"
		}
		if cacheSize > 0 {
			label += " (lru=" + strconv.Itoa(cacheSize) + ")"
		}
		return vault, anonStore, label, func() {
			_ = boltVault.Close()
			_ = anonStore.Close()
			_ = db.Close()
		}
	default:
		log.Fatalf("unsupported STORE_BACKEND=%q (valid: memory, bbolt)", backend)
		return nil, nil, "", func() {}
	}
}

// resolveBoltPath implements the bbolt path precedence documented on
// selectStoreBackend: ANONDE_DATA_DIR/anonde.db when the anchor is
// set, falling back to "anonde.db" in the process CWD.
//
// The anchor matches the telemetry install_id layout so one mounted
// volume (typically /var/lib/anonde in the shipped images) holds
// both files with no subdirectory nesting and only one env var for
// operators to remember.
func resolveBoltPath() string {
	if anchor := strings.TrimSpace(os.Getenv("ANONDE_DATA_DIR")); anchor != "" {
		return anchor + string(os.PathSeparator) + "anonde.db"
	}
	return "anonde.db"
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		log.Fatalf("invalid duration for %s=%q: %v", key, raw, err)
	}
	return parsed
}

func intFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Fatalf("invalid integer for %s=%q: %v", key, raw, err)
	}
	if n < 0 {
		log.Fatalf("invalid integer for %s=%d: must be >= 0", key, n)
	}
	return n
}

func bytesFromEnv(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Fatalf("invalid byte count for %s=%q: %v", key, raw, err)
	}
	if n < 0 {
		log.Fatalf("invalid byte count for %s=%d: must be >= 0", key, n)
	}
	return n
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
