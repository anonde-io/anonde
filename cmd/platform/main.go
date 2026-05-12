package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moogacs/anonde"
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/internal/platform"
)

func main() {
	addr := platformAddr()
	analyzerEngine := analyzerFromEnv()

	// One-shot bootstrap used by Dockerfile.platform-ner: initialise the
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

	vaultTTL := durationFromEnv("MEMORY_VAULT_TTL", 5*time.Minute)
	storeTTL := durationFromEnv("MEMORY_STORE_TTL", 5*time.Minute)
	maxBytes := bytesFromEnv("MAX_CONTENT_BYTES", platform.DefaultMaxRequestBytes)

	svc := platform.NewService(
		analyzerEngine,
		anonde.DefaultAnonymizerEngine(),
		platform.NewMemoryVaultWithTTL(vaultTTL),
		platform.NewMemoryStoreWithTTL(storeTTL),
		&platform.StaticPolicy{},
	)

	httpAPI := platform.NewHTTPServer(svc)
	httpAPI.SetMaxRequestBytes(maxBytes)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           httpAPI.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("platform server listening on %s (max_request_bytes=%d)", addr, maxBytes)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// warmupAnalyzer forces the analyzer engine's lazy initialisation paths
// (notably the Hugot ONNX session under sync.Once) to run before the HTTP
// server starts listening. On failure the process exits with the analyzer
// error, surfacing under-provisioning (OOM) at boot rather than as a hung
// first user request.
//
// The 5 minute timeout accommodates the worst-case cold disk read +
// pure-Go ONNX session init on the smallest Fly machine. Tighten via
// PLATFORM_WARMUP_TIMEOUT if you have a tighter SLA for boot latency.
func warmupAnalyzer(engine *analyzer.AnalyzerEngine) {
	timeout := durationFromEnv("PLATFORM_WARMUP_TIMEOUT", 5*time.Minute)
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
// exits. Used at Docker build time (see Dockerfile.platform-ner) to bake
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

func platformAddr() string {
	if addr := strings.TrimSpace(os.ExpandEnv(os.Getenv("PLATFORM_ADDR"))); addr != "" {
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
// This is the fast / safe default and what Dockerfile.platform ships. The
// German recognizer kernel covers most clinical PII without NER.
//
// Opt-ins:
//
//   - ANALYZER_BACKEND=hugot — in-process ONNX transformer. Requires the
//     binary to be built with `-tags hugot`; default builds exclude the
//     hugot dependency graph entirely. Setting this on a no-hugot binary
//     fails fast with a clear remediation message.
//   - ANALYZER_BACKEND=ollama — local Ollama daemon for users with an
//     existing Ollama setup.
//
// Presidio is no longer a runtime backend. To benchmark anonde against
// Presidio, see bench/parity/.
func analyzerFromEnv() *analyzer.AnalyzerEngine {
	backend := strings.ToLower(strings.TrimSpace(getenvDefault("ANALYZER_BACKEND", "patterns")))
	switch backend {
	case "patterns", "patterns-only", "":
		log.Printf("analyzer backend: patterns-only (no NER)")
		return anonde.DefaultAnalyzerEngine()
	case "ollama":
		log.Printf("analyzer backend: ollama")
		return anonde.DefaultAnalyzerEngineWithOllama(
			os.Getenv("OLLAMA_ENDPOINT"),
			os.Getenv("OLLAMA_MODEL"),
		)
	case "hugot":
		modelName := getenvDefault("HUGOT_MODEL", "Isotonic/distilbert_finetuned_ai4privacy_v2")
		log.Printf("analyzer backend: hugot (model=%s)", modelName)
		return anonde.DefaultAnalyzerEngineWithHugot(
			os.Getenv("HUGOT_MODELS_DIR"),
			modelName,
			true, // auto-download on first run
		)
	default:
		log.Fatalf("unsupported ANALYZER_BACKEND=%q (valid: patterns, hugot, ollama)", backend)
		return nil
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
