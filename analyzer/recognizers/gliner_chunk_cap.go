package recognizers

import "fmt"

// chunkCapError returns the fail-closed error that aborts a GLiNER Analyze
// when the input needs more sliding-window chunks than the active cap allows,
// or nil when the input fits (or the cap is disabled, maxChunks <= 0).
//
// Failing closed is deliberate. Silently NER-covering only the first
// maxChunks chunks leaves the document's tail with pattern-only coverage,
// dropping the PERSON / ORGANIZATION / LOCATION spans that GLiNER alone
// catches — a silent leak on long documents. The analyzer turns a
// model-backed recognizer error into a failed request, so a long document is
// never silently under-covered instead of loudly rejected. Operators who need
// to process larger inputs raise GLiNERConfig.MaxChunks (accepting the added
// latency) or set it negative to disable the cap; a caller that deliberately
// wants pattern-only coverage passes disable_ner.
//
// Kept in an untagged file (the GLiNER recognizer itself is -tags ner + CGO)
// so this leak-safety decision is unit-testable without loading an ONNX model.
func chunkCapError(nChunks, maxChunks, textBytes int) error {
	if maxChunks > 0 && nChunks > maxChunks {
		return fmt.Errorf(
			"gliner: input needs %d chunks, exceeds max_chunks=%d (text_bytes=%d); "+
				"raise GLiNERConfig.MaxChunks or set it negative to disable the cap, or pass disable_ner",
			nChunks, maxChunks, textBytes)
	}
	return nil
}
