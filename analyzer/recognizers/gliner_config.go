// GLiNERConfig and the label sets are pure-data declarations, so this file
// is NOT build-tagged: it is referenced in non-ner builds (the stub
// DefaultAnalyzerEngineWithGLiNERConfig keeps the public API stable across
// build variants).

package recognizers

// GLiNERConfig configures the Go-native GLiNER recognizer.
//
// GLiNER is a open-set / zero-shot NER architecture: the label list is
// supplied at inference time, not baked into the model weights. The
// recognizer wraps an ONNX export of the model and runs entirely
// in-process with no Python sidecar. See ner_gliner.go for the
// implementation (build-tagged `ner`).
type GLiNERConfig struct {
	// ModelsDir is the local directory where models are stored.
	// Defaults to ~/.cache/anonde/models.
	ModelsDir string

	// ModelName is the HuggingFace model ID to use.
	// Defaults to "knowledgator/gliner-pii-base-v1.0"; a DeBERTa-v3-small
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
	// present locally. The recognizer reuses the hugot library's
	// downloader so the on-disk cache layout matches hugot's.
	AutoDownload bool

	// Labels lists the open-set entity labels to score at inference.
	// Empty uses DefaultPIILabels (= ChatPIILabels, see defaultGLiNERLabels);
	// clinical/HIPAA callers set ClinicalPIILabels for AGE/DATE/clinical
	// coverage. Tuned per-model; GLiNER's zero-shot recall is sensitive to
	// label phrasing.
	Labels []string

	// LabelToEntity maps each prompt label (as it appears in Labels) to
	// the anonde canonical entity type. Empty uses ChatPIILabelToEntity.
	// Labels not in the map are dropped at result time.
	LabelToEntity map[string]string

	// Threshold filters spans whose sigmoid(logit) is below this value.
	// Defaults to 0.40 (matches the Python sidecar).
	Threshold float64

	// ClassThresholds overrides the per-class score floor, keyed by
	// canonical entity type. A present value is used DIRECTLY (not min()'d
	// against Threshold), so it can RAISE a floor the built-in min() never
	// can — e.g. PERSON above the compiled-in 0.22 to stop the model firing
	// common words as PERSON. Absent classes keep their built-in floor; nil
	// preserves today's behaviour. Both the span and flat recognizers
	// consult this map.
	ClassThresholds map[string]float64

	// SpanFilter post-validates every decoded NER span (see
	// span_shape_filter.go): a universal money guard (MoneyGuard) plus the
	// opt-in structural shape filter (Enabled). MoneyGuardFilter() is the
	// default NER profile; StrictSpanFilter() / LegalSpanFilter() add the
	// shape layer. Zero value is a no-op. Consulted by every GLiNER variant.
	SpanFilter SpanFilterConfig

	// MaxWidth caps span width in WORDS (not subword tokens). Defaults to
	// 12, matching the model's `gliner_config.json::max_width`. Setting
	// this lower than the trained value can hurt recall on long entities
	// (full names, addresses); higher values are silently truncated to
	// the trained value.
	MaxWidth int

	// MaxTokens is the upper bound on the encoder sequence length per
	// chunk (subword tokens including the prompt prefix + specials).
	// Defaults to 384; leaves headroom inside DeBERTa-v3's 512-token
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

	// MaxChunks caps the number of sliding-window chunks one Analyze()
	// call inferences through. Documents that exceed the cap get
	// partial NER coverage (the first MaxChunks chunks); pattern
	// recognizers still run on the full text so structured PII is
	// preserved end-to-end. Default 64 (~80 KB of text at the default
	// ChunkChars). Set to a negative value to disable the cap.
	MaxChunks int

	// SharedLibraryPath optionally overrides the onnxruntime shared
	// library location. Empty uses defaults (libonnxruntime.dylib
	// on macOS, libonnxruntime.so on Linux).
	SharedLibraryPath string
}

// defaultGLiNERLabels is the label set an empty GLiNERConfig.Labels resolves
// to. The default IS the chat set (DefaultPIILabels = ChatPIILabels): no-label
// callers get chat, which drops AGE/PROFESSION/DATE/clinical labels that
// over-fire on conversation. Clinical/HIPAA opts back in via
// GLINER_LABEL_SET=clinical → ClinicalPIILabels (the bench runner pins it).
func defaultGLiNERLabels() []string { return DefaultPIILabels }

// defaultGLiNERLabelToEntity is the companion default for an empty
// GLiNERConfig.LabelToEntity, kept in lock-step with defaultGLiNERLabels.
func defaultGLiNERLabelToEntity() map[string]string { return DefaultLabelToEntity }

// DefaultPIILabels is THE default label set — the chat set — used by any
// no-label caller (empty GLiNERConfig.Labels). Aliased to ChatPIILabels so the
// default tracks chat tuning. Clinical/legal/finance callers select their
// domain set explicitly (GLINER_LABEL_SET / *PIILabels).
var DefaultPIILabels = ChatPIILabels

// DefaultLabelToEntity is the label→canonical map for DefaultPIILabels; aliased
// to ChatPIILabelToEntity, kept in lock-step with DefaultPIILabels.
var DefaultLabelToEntity = ChatPIILabelToEntity

// ClinicalPIILabels is the curated clinical/HIPAA PII label set, also mirrored
// by the Python sidecar (bench/runners/gliner.py). It carries the broad
// coverage chat drops: age, profession, date(/of birth), the clinical labels
// (patient/doctor/hospital), and the German insurance/tax/case-file IDs. Select
// via GLINER_LABEL_SET=clinical.
//
// Order matters only for determinism; the model treats labels as a set.
var ClinicalPIILabels = []string{
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
	"Versicherungsnummer",
	"Krankenkassenname",
	"Aktenzeichen",
	"Geburtsdatum",
	"Steuer-Identifikationsnummer",
	"Personalausweisnummer",
}

// ClinicalLabelToEntity mirrors LABEL_TO_CANONICAL in bench/runners/gliner.py:
// it maps each ClinicalPIILabels label to the anonde canonical entity type, the
// same identifiers the pattern recognizers emit. Keep in lock-step with the
// Python sidecar so cross-engine comparisons stay apples-to-apples.
var ClinicalLabelToEntity = map[string]string{
	"person":                       "PERSON",
	"first name":                   "PERSON",
	"last name":                    "PERSON",
	"full name":                    "PERSON",
	"patient name":                 "PERSON",
	"doctor name":                  "PERSON",
	"organization":                 "ORGANIZATION",
	"company":                      "ORGANIZATION",
	"hospital":                     "ORGANIZATION",
	"city":                         "LOCATION",
	"country":                      "LOCATION",
	"state":                        "LOCATION",
	"address":                      "ADDRESS",
	"street address":               "STREET_ADDRESS",
	"street":                       "STREET_ADDRESS",
	"building number":              "STREET_ADDRESS",
	"postal code":                  "POSTAL_CODE",
	"zip code":                     "POSTAL_CODE",
	"date":                         "DATE_TIME",
	"date of birth":                "DATE_TIME",
	"phone number":                 "PHONE_NUMBER",
	"email":                        "EMAIL_ADDRESS",
	"email address":                "EMAIL_ADDRESS",
	"url":                          "URL",
	"credit card":                  "CREDIT_CARD",
	"credit card number":           "CREDIT_CARD",
	"iban":                         "IBAN_CODE",
	"ssn":                          "US_SSN",
	"passport":                     "ID",
	"social security number":       "US_SSN",
	"id number":                    "ID",
	"age":                          "AGE",
	"profession":                   "PROFESSION",
	"job title":                    "PROFESSION",
	"Versicherungsnummer":          "ID",
	"Krankenkassenname":            "ORGANIZATION",
	"Aktenzeichen":                 "ID",
	"Geburtsdatum":                 "DATE_TIME",
	"Steuer-Identifikationsnummer": "ID",
	"Personalausweisnummer":        "ID",
}

// ChatPIILabels is tuned for casual / conversational traffic, NOT clinical or
// legal docs. It drops the noisy-in-chat labels ClinicalPIILabels carries —
// age, profession, job title, date(/of birth), the clinical labels, and the
// German insurance/tax/case-file IDs — which over-fire on conversation (dogfood
// saw "18 years of experience" tagged AGE, "tech" tagged PROFESSION). Retained:
// names, org/company, contact handles, postal geography, and structured
// financial/government IDs. Select via GLINER_LABEL_SET=chat.
// Order is for determinism only; the model treats labels as a set.
var ChatPIILabels = []string{
	"person",
	"first name",
	"last name",
	"full name",
	"organization",
	"company",
	"email",
	"email address",
	"phone number",
	"url",
	"address",
	"street address",
	"street",
	"city",
	"country",
	"state",
	"postal code",
	"zip code",
	"credit card",
	"credit card number",
	"iban",
	"ssn",
	"social security number",
	"passport",
	"id number",
}

// ChatPIILabelToEntity maps each ChatPIILabels label to its canonical entity
// type. A strict subset of ClinicalLabelToEntity (same mappings, minus the
// dropped clinical/AGE/PROFESSION/DATE labels).
var ChatPIILabelToEntity = map[string]string{
	"person":                 "PERSON",
	"first name":             "PERSON",
	"last name":              "PERSON",
	"full name":              "PERSON",
	"organization":           "ORGANIZATION",
	"company":                "ORGANIZATION",
	"email":                  "EMAIL_ADDRESS",
	"email address":          "EMAIL_ADDRESS",
	"phone number":           "PHONE_NUMBER",
	"url":                    "URL",
	"address":                "ADDRESS",
	"street address":         "STREET_ADDRESS",
	"street":                 "STREET_ADDRESS",
	"city":                   "LOCATION",
	"country":                "LOCATION",
	"state":                  "LOCATION",
	"postal code":            "POSTAL_CODE",
	"zip code":               "POSTAL_CODE",
	"credit card":            "CREDIT_CARD",
	"credit card number":     "CREDIT_CARD",
	"iban":                   "IBAN_CODE",
	"ssn":                    "US_SSN",
	"social security number": "US_SSN",
	"passport":               "ID",
	"id number":              "ID",
}

// FinancePIILabels is tuned for financial documents — bank statements, KYC
// onboarding, payment records, tax forms. It keeps the identity+contact core
// (person, organization, email, phone) and adds the structured financial IDs:
// bank account/routing numbers, IBAN, SWIFT/BIC, card PAN+CVV, tax IDs
// (SSN/ITIN/EIN/Steuer-ID), and account/transaction identifiers. Drops
// profession/job title/age. All labels map to existing canonical types; see
// FinancePIILabelToEntity for the coarse folds (routing/SWIFT/EIN/transaction
// → ID, CVV → CREDIT_CARD).
// Order is for determinism only; the model treats labels as a set.
var FinancePIILabels = []string{
	"person",
	"first name",
	"last name",
	"full name",
	"account holder",
	"organization",
	"company",
	"bank name",
	"email",
	"email address",
	"phone number",
	"bank account number",
	"account number",
	"routing number",
	"iban",
	"swift code",
	"bic",
	"credit card",
	"credit card number",
	"cvv",
	"tax identification number",
	"ein",
	"employer identification number",
	"ssn",
	"social security number",
	"itin",
	"brokerage account number",
	"investment account number",
	"transaction id",
}

// FinancePIILabelToEntity maps each FinancePIILabels label to its canonical
// entity type. Finance-specific labels with no dedicated type fold into the
// generic "ID" (routing/SWIFT/BIC/EIN/brokerage/transaction) or share an
// operator (account holder → PERSON, cvv → CREDIT_CARD, bank name → ORG).
var FinancePIILabelToEntity = map[string]string{
	"person":                         "PERSON",
	"first name":                     "PERSON",
	"last name":                      "PERSON",
	"full name":                      "PERSON",
	"account holder":                 "PERSON",
	"organization":                   "ORGANIZATION",
	"company":                        "ORGANIZATION",
	"bank name":                      "ORGANIZATION",
	"email":                          "EMAIL_ADDRESS",
	"email address":                  "EMAIL_ADDRESS",
	"phone number":                   "PHONE_NUMBER",
	"bank account number":            "US_BANK_NUMBER",
	"account number":                 "US_BANK_NUMBER",
	"routing number":                 "ID",
	"iban":                           "IBAN_CODE",
	"swift code":                     "ID",
	"bic":                            "ID",
	"credit card":                    "CREDIT_CARD",
	"credit card number":             "CREDIT_CARD",
	"cvv":                            "CREDIT_CARD",
	"tax identification number":      "ID",
	"ein":                            "ID",
	"employer identification number": "ID",
	"ssn":                            "US_SSN",
	"social security number":         "US_SSN",
	"itin":                           "US_ITIN",
	"brokerage account number":       "ID",
	"investment account number":      "ID",
	"transaction id":                 "ID",
}

// LegalPIILabels is tuned for legal documents — pleadings, contracts, court
// filings, matter files. It keeps the identity+contact+geography core and,
// crucially, KEEPS date / date of birth (legal docs are date-sensitive, unlike
// chat). It adds the legal-specific IDs: case/docket/matter/contract/bar
// numbers, court name, and party roles (attorney/counsel/plaintiff/defendant/
// judge). All map to existing canonical types: the IDs fold into "ID", party
// roles into PERSON, court name into ORGANIZATION.
// Order is for determinism only; the model treats labels as a set.
var LegalPIILabels = []string{
	"person",
	"first name",
	"last name",
	"full name",
	"attorney",
	"counsel",
	"plaintiff",
	"defendant",
	"judge",
	"organization",
	"company",
	"court name",
	"email",
	"email address",
	"phone number",
	"address",
	"street address",
	"city",
	"country",
	"state",
	"postal code",
	"date",
	"date of birth",
	"case number",
	"docket number",
	"matter number",
	"contract number",
	"bar number",
}

// LegalPIILabelToEntity maps each LegalPIILabels label to its canonical entity
// type. Legal IDs (case/docket/matter/contract/bar) fold into "ID"; party
// roles into PERSON; court name into ORGANIZATION; date/DOB into DATE_TIME
// (retained on purpose, unlike the chat set).
var LegalPIILabelToEntity = map[string]string{
	"person":          "PERSON",
	"first name":      "PERSON",
	"last name":       "PERSON",
	"full name":       "PERSON",
	"attorney":        "PERSON",
	"counsel":         "PERSON",
	"plaintiff":       "PERSON",
	"defendant":       "PERSON",
	"judge":           "PERSON",
	"organization":    "ORGANIZATION",
	"company":         "ORGANIZATION",
	"court name":      "ORGANIZATION",
	"email":           "EMAIL_ADDRESS",
	"email address":   "EMAIL_ADDRESS",
	"phone number":    "PHONE_NUMBER",
	"address":         "ADDRESS",
	"street address":  "STREET_ADDRESS",
	"city":            "LOCATION",
	"country":         "LOCATION",
	"state":           "LOCATION",
	"postal code":     "POSTAL_CODE",
	"date":            "DATE_TIME",
	"date of birth":   "DATE_TIME",
	"case number":     "ID",
	"docket number":   "ID",
	"matter number":   "ID",
	"contract number": "ID",
	"bar number":      "ID",
}
