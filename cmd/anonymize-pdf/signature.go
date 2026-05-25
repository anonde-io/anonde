package main

import (
	"fmt"

	"github.com/anonde-io/anonde/internal/content"
)

// loadSignatureDetector returns a "not yet wired" error in this PR.
// The real implementation lands in the next PR of the stack, which
// adds content.LoadSignatureDetector (YOLOS via onnxruntime_go).
func loadSignatureDetector(_ string) (content.VisualDetector, error) {
	return nil, fmt.Errorf("--signature-model is not wired in this build (see next PR in the stack)")
}
