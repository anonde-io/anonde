package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
	"github.com/anonde-io/anonde/internal/api"
	"github.com/anonde-io/anonde/internal/content"
	"github.com/anonde-io/anonde/internal/core"
	"github.com/anonde-io/anonde/internal/policy"
	"github.com/anonde-io/anonde/internal/store"
)

func main() {
	addr := listenAddr()
	analyzerEngine, backendName, modelName := analyzerFromEnv()

	// One-shot bootstrap used by Dockerfile.anonde-ner: initialise the
	// active analyzer, run one trivial inference call to force the NER
	// backend to download / cache its model into HUGOT_MODELS_DIR, then
	// exit cleanly. The runtime image then ships with the model on disk
	// and never needs network access at startup.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DOWNLOAD_MODELS_ONLY")), "1") {
		downloadModelsAndExit(analyzerEngine)
	}

	// WARMUP_ON_START=1 runs one trivial Analyze synchronously before the
	// HTTP server starts listening. For NER backends this forces the
	// sync.Once-gated ONNX session creation to happen at boot — failure
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

	svc := core.NewService(
		analyzerEngine,
		anonde.DefaultAnonymizerEngine(),
		vault,
		anonStore,
		&policy.Static{},
	)
	svc.SetVersionInfo(core.VersionInfo{
		AnalyzerBackend: backendName,
		Model:           modelName,
		BuildSHA:        buildSHA(),
		GoVersion:       runtime.Version(),
		APIVersion:      "v1",
	})

	httpAPI := api.NewHTTPServer(svc)
	httpAPI.SetMaxRequestBytes(maxBytes)

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
	// via ANONDE_PDF_ENABLED=1 because it: (a) requires pdftoppm +
	// tesseract on PATH for OCR fallback on scanned PDFs, and (b)
	// optionally loads a ~250 MB YOLOS signature model when
	// ANONDE_PDF_VISION_MODEL=1 is set. Endpoint returns 501 with a
	// pointer at this env var when disabled.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ANONDE_PDF_ENABLED")), "1") {
		opts := content.RedactPDFOptions{
			Engine:          analyzerEngine,
			AnalysisCfg:     analyzer.AnalysisConfig{ScoreThreshold: 0.30, RemoveConflicts: true},
			DPI:             200,
			BoxPadding:      2,
			VisualHeuristic: true,
		}
		if strings.EqualFold(strings.TrimSpace(os.Getenv("ANONDE_PDF_VISION_MODEL")), "1") {
			detector, derr := content.LoadSignatureDetector(strings.TrimSpace(os.Getenv("ANONDE_SIGNATURE_MODEL_PATH")))
			if derr != nil {
				log.Fatalf("ANONDE_PDF_VISION_MODEL=1 but failed to load signature detector: %v", derr)
			}
			opts.VisualDetector = detector
			log.Printf("pdf endpoint: signature detector loaded")
		}
		httpAPI.SetPDFRedactor(api.NewPDFRedactor(opts))
		log.Printf("pdf endpoint enabled at POST /v1/anonymizations/pdf + GET /v1/anonymizations/{id}/reveal-pdf (vision_model=%s)",
			strings.TrimSpace(os.Getenv("ANONDE_PDF_VISION_MODEL")))
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           httpAPI.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		Protocols:         api.NewServerProtocols(),
	}

	log.Printf("anonde server listening on %s (max_request_bytes=%d backend=%s model=%s store=%s)",
		addr, maxBytes, backendName, modelName, storeName)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
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

// warmupAnalyzer forces the analyzer engine's lazy initialisation paths
// (notably the Hugot ONNX session under sync.Once) to run before the HTTP
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
}

// downloadModelsAndExit triggers a single inference call so the configured
// backend's underlying NER model is fetched into its cache directory, then
// exits. Used at Docker build time (see Dockerfile.anonde-ner) to bake
// the model into the runtime image — the runtime then has no cold-start
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
//   - ANALYZER_BACKEND=hugot — in-process ONNX transformer via hugot (XLM-R
//     PII multilingual by default). Requires the binary to be built with
//     `-tags hugot`; default builds fall back to a fatal-error stub.
//   - ANALYZER_BACKEND=gliner — in-process GLiNER PII via yalue/onnxruntime_go.
//     Open-set NER, substantially better German clinical recall than hugot
//     (see bench/corpora/openmed/REPORT_FINAL.md). Requires `-tags hugot` AND
//     CGO_ENABLED=1 AND libonnxruntime.so reachable at runtime.
//   - ANALYZER_BACKEND=ollama — local Ollama daemon for users with an
//     existing Ollama setup.
//
// Presidio is no longer a runtime backend. To benchmark anonde against
// Presidio, see bench/corpora/ai4privacy_en/.
func analyzerFromEnv() (*analyzer.AnalyzerEngine, string, string) {
	backend := strings.ToLower(strings.TrimSpace(getenvDefault("ANALYZER_BACKEND", "patterns")))
	switch backend {
	case "patterns", "patterns-only", "":
		log.Printf("analyzer backend: patterns-only (no NER)")
		return anonde.DefaultAnalyzerEngine(), "patterns", ""
	case "ollama":
		modelName := os.Getenv("OLLAMA_MODEL")
		log.Printf("analyzer backend: ollama")
		return anonde.DefaultAnalyzerEngineWithOllama(
			os.Getenv("OLLAMA_ENDPOINT"),
			modelName,
		), "ollama", modelName
	case "hugot":
		modelName := getenvDefault("HUGOT_MODEL", "Isotonic/distilbert_finetuned_ai4privacy_v2")
		log.Printf("analyzer backend: hugot (model=%s)", modelName)
		return anonde.DefaultAnalyzerEngineWithHugot(
			os.Getenv("HUGOT_MODELS_DIR"),
			modelName,
			true, // auto-download on first run
		), "hugot", modelName
	case "gliner":
		modelName := getenvDefault("GLINER_MODEL", "onnx-community/gliner_multi_pii-v1")
		onnxPath := glinerOnnxFileFromEnv(modelName)
		// Threshold is the most impactful knob — multilingual variants
		// typically need ~0.25, the English base ~0.40. Override without
		// rebuilding via GLINER_THRESHOLD. Zero = recognizer default (0.40).
		threshold := 0.0
		if raw := strings.TrimSpace(os.Getenv("GLINER_THRESHOLD")); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				threshold = v
			} else {
				log.Printf("GLINER_THRESHOLD=%q ignored: %v", raw, err)
			}
		}
		// Multi-model GLiNER stacking: if ANONDE_NER_STACK is set,
		// build an ensemble across the listed model IDs instead of the
		// single-model path. Unset → fall through to current behaviour
		// with no change. EnsembleFromEnv returns (nil, nil) on unset
		// and (nil, error) on a malformed value (e.g. ",,,") so a typo
		// fails fast at boot rather than silently disabling NER.
		if ens, ensErr := recognizers.EnsembleFromEnv(threshold, os.Getenv("ORT_SO_PATH")); ensErr != nil {
			log.Fatalf("ANONDE_NER_STACK: %v", ensErr)
		} else if ens != nil {
			log.Printf("analyzer backend: gliner-ensemble (threshold=%.2f) — single-model GLINER_MODEL=%s ignored",
				threshold, modelName)
			return anonde.DefaultAnalyzerEngineWithGLiNEREnsemble(ens), "gliner-ensemble", os.Getenv("ANONDE_NER_STACK")
		}
		log.Printf("analyzer backend: gliner (model=%s, onnx=%s, threshold=%.2f)", modelName, onnxPath, threshold)
		return anonde.DefaultAnalyzerEngineWithGLiNERConfig(recognizers.GLiNERConfig{
			ModelsDir:         os.Getenv("GLINER_MODELS_DIR"),
			ModelName:         modelName,
			OnnxFilePath:      onnxPath,
			AutoDownload:      true,
			SharedLibraryPath: os.Getenv("ORT_SO_PATH"),
			Threshold:         threshold,
			// Labels left empty → DefaultPIILabels.
		}), "gliner", modelName
	default:
		log.Fatalf("unsupported ANALYZER_BACKEND=%q (valid: patterns, hugot, gliner, ollama)", backend)
		return nil, "", ""
	}
}

// glinerOnnxFileFromEnv resolves which ONNX export of the GLiNER model
// to load. The GLiNER HF repos ship multiple quantization variants of
// the same weights; the choice is a recall / size / latency trade-off.
//
// Two env vars, in precedence order:
//
//   - GLINER_ONNX_FILE — explicit in-repo path (e.g. "onnx/model.onnx").
//     The escape hatch: use it for any repo whose file layout doesn't
//     match the convenience names below.
//   - GLINER_QUANT — convenience selector: "int8" | "fp16" | "fp32".
//     Maps to the conventional filenames in the knowledgator and
//     onnx-community GLiNER repos. FP32 is the default — production
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
		// graph — onnxruntime rejects it on session create with a
		// "Type Error: Type (tensor(float16)) of output arg ... Cast
		// ..." failure, and the GLiNER recognizer then silently falls
		// back to patterns-only. The mapping is kept (a future fixed
		// FP16 export from the upstream repo may load cleanly), but as
		// of now GLINER_QUANT=fp16 does NOT work for this model — use
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
// "bbolt" defaults the on-disk file to ANONDE_BBOLT_PATH (override:
// any writable path; the default "anonde.db" lands in the process
// CWD). At-rest encryption is opt-in: set ANONDE_VAULT_KEY to a
// base64 32-byte key to enable AES-256-GCM, otherwise vault values
// are stored as plaintext JSON. We log loudly when encryption is off
// so an operator can't miss that the file holds raw PII.
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
		path := strings.TrimSpace(getenvDefault("ANONDE_BBOLT_PATH", "anonde.db"))
		var key []byte
		if strings.TrimSpace(os.Getenv("ANONDE_VAULT_KEY")) != "" {
			var err error
			key, err = store.LoadVaultKey()
			if err != nil {
				log.Fatalf("STORE_BACKEND=bbolt: %v", err)
			}
		} else {
			log.Printf("STORE_BACKEND=bbolt: ANONDE_VAULT_KEY not set — vault stored UNENCRYPTED at %s", path)
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
