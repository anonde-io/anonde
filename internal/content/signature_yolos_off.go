//go:build !ner

package content

import "fmt"

// LoadSignatureDetector returns a stub error when built without
// -tags ner. The YOLOS signature model runs via yalue/onnxruntime_go
// (CGO + libonnxruntime), the same dependency the GLiNER NER path
// requires, so it's gated on the same build tag.
func LoadSignatureDetector(_ string) (VisualDetector, error) {
	return nil, fmt.Errorf("signature model requires `go build -tags ner` and libonnxruntime")
}
