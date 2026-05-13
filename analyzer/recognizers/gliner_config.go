// gliner_config.go is intentionally NOT build-tagged. GLiNERConfig and
// DefaultPIILabels are pure-data declarations (no hugot/onnxruntime
// dependency in any field type), so they can be defined and referenced in
// non-hugot builds — the stub `DefaultAnalyzerEngineWithGLiNERConfig` in
// ../../hugot_off.go uses GLiNERConfig in its signature so the public API
// surface stays stable across build variants.

package recognizers

// GLiNERConfig configures the Go-native GLiNER recognizer.
//
// GLiNER is a open-set / zero-shot NER architecture: the label list is
// supplied at inference time, not baked into the model weights. The
// recognizer wraps an ONNX export of the model and runs entirely
// in-process with no Python sidecar. See ner_gliner.go for the
// implementation (build-tagged `hugot`).
type GLiNERConfig struct {
	// ModelsDir is the local directory where models are stored.
	// Defaults to ~/.cache/anonde/models.
	ModelsDir string

	// ModelName is the HuggingFace model ID to use.
	// Defaults to "knowledgator/gliner-pii-base-v1.0" — a DeBERTa-v3-small
	// uni-encoder span-level GLiNER fine-tuned on broad PII data.
	//
	// Any GLiNER uni-encoder-span ONNX export with the same input/output
	// signature (`input_ids`, `attention_mask`, `words_mask`,
	// `text_lengths`, `span_idx`, `span_mask` → `logits`) should work, but
	// the prompt format and tokenizer expectations (DeBERTa-style added
	// tokens `<<ENT>>` / `<<SEP>>`) are hard-coded here.
	ModelName string

	// OnnxFilePath optionally selects a specific ONNX file inside the model
	// repo (e.g. "onnx/model_quint8.onnx" for the int8-quantised build).
	// Empty selects the repo default.
	OnnxFilePath string

	// AutoDownload, when true, downloads the model on first use if not
	// present locally. The recognizer reuses hugot's downloader so the
	// on-disk layout matches HugotNERRecognizer's.
	AutoDownload bool

	// Labels lists the open-set entity labels to score at inference.
	// Empty uses DefaultPIILabels. The list is tuned per-model; GLiNER's
	// zero-shot recall is sensitive to label phrasing.
	Labels []string

	// LabelToEntity maps each prompt label (as it appears in Labels) to
	// the anonde canonical entity type. Empty uses DefaultLabelToEntity.
	// Labels not in the map are dropped at result time.
	LabelToEntity map[string]string

	// Threshold filters spans whose sigmoid(logit) is below this value.
	// Defaults to 0.40 (matches the Python sidecar).
	Threshold float64

	// MaxWidth caps span width in WORDS (not subword tokens). Defaults to
	// 12, matching the model's `gliner_config.json::max_width`. Setting
	// this lower than the trained value can hurt recall on long entities
	// (full names, addresses); higher values are silently truncated to
	// the trained value.
	MaxWidth int

	// MaxTokens is the upper bound on the encoder sequence length per
	// chunk (subword tokens including the prompt prefix + specials).
	// Defaults to 384 — leaves headroom inside DeBERTa-v3's 512-token
	// position embedding. Lower values shrink each chunk and increase
	// the chunk count.
	MaxTokens int

	// ChunkChars is the maximum byte size of each sliding-window chunk
	// of the input text. Larger documents are split on whitespace
	// boundaries with overlap. Zero uses an internal default.
	ChunkChars int

	// ChunkOverlap is the byte overlap between adjacent chunks. Zero
	// uses an internal default.
	ChunkOverlap int

	// SharedLibraryPath optionally overrides the onnxruntime shared
	// library location. Empty uses platform defaults (libonnxruntime.dylib
	// on macOS, libonnxruntime.so on Linux).
	SharedLibraryPath string
}

// DefaultPIILabels is the curated PII label set used by the Python
// sidecar (bench/runners/gliner.py). Tuning labels here changes
// recall; keep the list narrow to avoid noisy false positives, but wide
// enough to cover common anonde entity types.
//
// Order matters only for determinism — the model treats labels as a set.
var DefaultPIILabels = []string{
	"person",
	"first name",
	"last name",
	"full name",
	"patient name",
	"doctor name",
	"organization",
	"company",
	"hospital",
	"city",
	"country",
	"state",
	"address",
	"street address",
	"street",
	"building number",
	"postal code",
	"zip code",
	"date",
	"date of birth",
	"phone number",
	"email",
	"email address",
	"url",
	"credit card",
	"credit card number",
	"iban",
	"ssn",
	"passport",
	"social security number",
	"id number",
	"age",
	"profession",
	"job title",
}

// DefaultLabelToEntity mirrors LABEL_TO_CANONICAL in
// bench/runners/gliner.py: it maps each prompt label to the anonde
// canonical entity type, the same identifiers the pattern recognizers
// emit. Keep in lock-step with the Python sidecar so cross-engine
// comparisons stay apples-to-apples.
var DefaultLabelToEntity = map[string]string{
	"person":                 "PERSON",
	"first name":             "PERSON",
	"last name":              "PERSON",
	"full name":              "PERSON",
	"patient name":           "PERSON",
	"doctor name":            "PERSON",
	"organization":           "ORGANIZATION",
	"company":                "ORGANIZATION",
	"hospital":               "ORGANIZATION",
	"city":                   "LOCATION",
	"country":                "LOCATION",
	"state":                  "LOCATION",
	"address":                "ADDRESS",
	"street address":         "STREET_ADDRESS",
	"street":                 "STREET_ADDRESS",
	"building number":        "STREET_ADDRESS",
	"postal code":            "POSTAL_CODE",
	"zip code":               "POSTAL_CODE",
	"date":                   "DATE_TIME",
	"date of birth":          "DATE_TIME",
	"phone number":           "PHONE_NUMBER",
	"email":                  "EMAIL_ADDRESS",
	"email address":          "EMAIL_ADDRESS",
	"url":                    "URL",
	"credit card":            "CREDIT_CARD",
	"credit card number":     "CREDIT_CARD",
	"iban":                   "IBAN_CODE",
	"ssn":                    "US_SSN",
	"passport":               "ID",
	"social security number": "US_SSN",
	"id number":              "ID",
	"age":                    "AGE",
	"profession":             "PROFESSION",
	"job title":              "PROFESSION",
}
