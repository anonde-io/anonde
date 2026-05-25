package main

import "github.com/anonde-io/anonde/internal/content"

// loadSignatureDetector is now a thin wrapper around the shared
// content.LoadSignatureDetector so the CLI and the HTTP server share
// the same model loader and cache directory.
func loadSignatureDetector(overridePath string) (content.VisualDetector, error) {
	return content.LoadSignatureDetector(overridePath)
}
