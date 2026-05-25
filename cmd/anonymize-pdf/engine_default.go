//go:build !hugot

package main

import (
	"fmt"
	"os"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
)

// buildAnalyzerEngine returns a patterns-only engine. NER backends
// (hugot, gliner) require building this binary with `-tags hugot` AND
// having libonnxruntime reachable; see engine_hugot.go for that path.
func buildAnalyzerEngine(backend string) (*analyzer.AnalyzerEngine, string, error) {
	switch backend {
	case "", "auto", "patterns", "patterns-only":
		if backend == "auto" {
			fmt.Fprintln(os.Stderr, "anonymize-pdf: patterns-only build — rebuild with `go build -tags hugot ./cmd/anonymize-pdf` for GLiNER NER (catches names, orgs, locations).")
		}
		return anonde.DefaultAnalyzerEngine(), "patterns", nil
	case "hugot", "gliner":
		return nil, "", fmt.Errorf("backend %q requires rebuild with: go build -tags hugot ./cmd/anonymize-pdf", backend)
	default:
		return nil, "", fmt.Errorf("unknown backend %q (use patterns)", backend)
	}
}
