// hugot_config.go is intentionally NOT build-tagged. HugotNERConfig is a
// pure-data struct (no hugot library dependency in any field type), so it
// can be defined and referenced in non-hugot builds; the stub
// `DefaultAnalyzerEngineWithHugotConfig` in ../../hugot_off.go uses it to
// keep the public API surface stable across build variants.

package recognizers

// HugotNERConfig configures the hugot-backed NER recognizer.
type HugotNERConfig struct {
	// ModelsDir is the local directory where models are stored.
	// Defaults to ~/.cache/anonde/models.
	ModelsDir string

	// ModelName is the HuggingFace model ID to use.
	// Defaults to "onnx-community/multilang-pii-ner-ONNX"; XLM-RoBERTa-base
	// fine-tuned for PII detection across English, German, Italian, and
	// French. Substantially better recall on German clinical text than a
	// generic CoNLL-2003 NER because it was trained on PII-specific labels
	// (GIVENNAME / SURNAME / CITY / STREET / BUILDINGNUM / ZIPCODE / AGE / …).
	//
	// Alternative defaults worth knowing:
	//   * "Xenova/distilbert-base-multilingual-cased-ner-hrl"; smaller
	//     (~135 MB), CoNLL-2003 news-text labels (PER/LOC/ORG), wider
	//     language coverage but weaker on clinical text.
	//   * "Isotonic/distilbert_finetuned_ai4privacy_v2"; English-only,
	//     ai4privacy-tuned, highest core-entity F1 on the English bench
	//     (see bench/corpora/ai4privacy_en/REPORT.md).
	ModelName string

	// AutoDownload, when true, downloads the model on first use if not present locally.
	AutoDownload bool

	// OnnxFilePath optionally selects a specific ONNX file inside the model
	// repo (e.g. "onnx/model_quantized.onnx" or "onnx/model_int8.onnx" for
	// faster inference at minor accuracy cost). Empty = repo default.
	// Only effective on the first download; cached models reuse whatever
	// was downloaded previously.
	OnnxFilePath string

	// ChunkChars is the maximum byte size of each sliding-window chunk
	// fed to the model. Docs longer than ChunkChars are split on
	// whitespace boundaries; entities found in any chunk are emitted
	// with their global offsets in the original text.
	//
	// Zero uses the recognizer's internal default. This must be smaller
	// than the model's token context window in chars; a 512-token model
	// with roughly 4 chars/token (typical for German) caps useful values
	// near 1800. Smaller values trade throughput for safety on
	// dense-tokenizing scripts (Chinese, Japanese).
	ChunkChars int

	// ChunkOverlap is the byte overlap between adjacent chunks, so an
	// entity sitting on a boundary is seen whole by at least one
	// chunk. Must be < ChunkChars.
	ChunkOverlap int

	// ScoreFloor filters NER predictions below this confidence before
	// they reach the analyzer. Zero uses the recognizer's internal
	// default. Use a negative value to disable filtering.
	ScoreFloor float64
}
