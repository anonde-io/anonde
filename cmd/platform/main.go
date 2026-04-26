package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/moogacs/anonde"
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/internal/platform"
)

func main() {
	addr := platformAddr()
	analyzerEngine := analyzerFromEnv()

	svc := platform.NewService(
		analyzerEngine,
		anonde.DefaultAnonymizerEngine(),
		platform.NewMemoryVault(),
		platform.NewMemoryStore(),
		&platform.StaticPolicy{},
	)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           platform.NewHTTPServer(svc).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("platform server listening on %s", addr)
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

func analyzerFromEnv() *analyzer.AnalyzerEngine {
	backend := strings.ToLower(strings.TrimSpace(getenvDefault("ANALYZER_BACKEND", "prose")))
	if backend == "ollama" {
		log.Printf("analyzer backend: ollama")
		return anonde.DefaultAnalyzerEngineWithOllama(
			os.Getenv("OLLAMA_ENDPOINT"),
			os.Getenv("OLLAMA_MODEL"),
		)
	}
	if backend == "presidio" || backend == "presidio-remote" {
		log.Printf("analyzer backend: presidio-remote")
		return anonde.DefaultAnalyzerEngineWithPresidioRemote(
			os.Getenv("PRESIDIO_ENDPOINT"),
		)
	}

	log.Printf("analyzer backend: prose")
	return anonde.DefaultAnalyzerEngine()
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
