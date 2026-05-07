package main

import (
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
// Default: hugot (in-process ONNX transformer; downloads the model on first
// run into ~/.cache/anonde/models). This is the recommended path —
// benchmarks show parity with Presidio default on the core entities. See
// bench/parity/REPORT_FULL.md.
//
// Opt-ins:
//
//   - ANALYZER_BACKEND=patterns — pattern recognizers only, no NER. Use when
//     you can't tolerate a model download and the entities you care about
//     are all pattern-detectable (email, phone, IP, SSN, CC, etc.).
//   - ANALYZER_BACKEND=ollama — local Ollama daemon for users with an
//     existing Ollama setup.
//
// Presidio is no longer a runtime backend. To benchmark anonde against
// Presidio, see bench/parity/.
func analyzerFromEnv() *analyzer.AnalyzerEngine {
	backend := strings.ToLower(strings.TrimSpace(getenvDefault("ANALYZER_BACKEND", "hugot")))
	switch backend {
	case "patterns", "patterns-only":
		log.Printf("analyzer backend: patterns-only (no NER)")
		return anonde.DefaultAnalyzerEngine()
	case "ollama":
		log.Printf("analyzer backend: ollama")
		return anonde.DefaultAnalyzerEngineWithOllama(
			os.Getenv("OLLAMA_ENDPOINT"),
			os.Getenv("OLLAMA_MODEL"),
		)
	case "hugot", "":
		modelName := getenvDefault("HUGOT_MODEL", "Isotonic/distilbert_finetuned_ai4privacy_v2")
		log.Printf("analyzer backend: hugot (model=%s)", modelName)
		return anonde.DefaultAnalyzerEngineWithHugot(
			os.Getenv("HUGOT_MODELS_DIR"),
			modelName,
			true, // auto-download on first run
		)
	default:
		log.Fatalf("unsupported ANALYZER_BACKEND=%q (valid: hugot, patterns, ollama)", backend)
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
