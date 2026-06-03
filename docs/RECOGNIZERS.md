# Recognizers

anonde ships **52 pattern recognizers** plus pluggable **NER backends**. Pattern recognizers handle structured PII (IDs, phone numbers, emails, IBANs); NER catches unstructured PII (people, organisations, locations, ages, professions).

## Pattern recognizers (52)

Score is each pattern's confidence band; recognizers that validate via checksums score higher than pure-regex ones.

| Region | Recognizers |
|---|---|
| Generic / international | `EmailRecognizer` · `PhoneRecognizer` · `CreditCardRecognizer` (Luhn) · `IBANRecognizer` (MOD-97) · `IPAddressRecognizer` · `MACAddressRecognizer` · `URLRecognizer` · `CryptoRecognizer` (Bitcoin/Ethereum) · `DateTimeRecognizer` |
| English / US | `USSocialSecurityRecognizer` · `USPassportRecognizer` · `USBankRecognizer` · `USDriverLicenseRecognizer` · `USITINRecognizer` · `MedicalLicenseRecognizer` · `ENAnomalyRecognizer` (clinical-context PERSON) · `ENOrganizationRecognizer` |
| United Kingdom | `UKNHSRecognizer` · `UKNINORecognizer` |
| Italy | `ITFiscalCodeRecognizer` · `ITDriverLicenseRecognizer` · `ITVATCodeRecognizer` · `ITPassportRecognizer` · `ITIdentityCardRecognizer` |
| Spain | `ESNIFRecognizer` · `ESNIERecognizer` |
| Australia | `AUABNRecognizer` · `AUACNRecognizer` · `AUTFNRecognizer` · `AUMedicareRecognizer` |
| India | `INPANRecognizer` · `INAadhaarRecognizer` · `INVehicleRegistrationRecognizer` · `INVoterRecognizer` · `INPassportRecognizer` |
| Poland | `PLPESELRecognizer` |
| Singapore | `SGNRICRecognizer` · `SGUENRecognizer` |
| Finland | `FIPersonalIdentityCodeRecognizer` |
| Korea | `KRRRNRecognizer` |
| Germany (first-class) | `DEDateTimeRecognizer` · `DEDateContextRecognizer` · `DEPhoneRecognizer` · `DEPostalCodeRecognizer` · `DEStreetRecognizer` · `DESteuerIDRecognizer` · `DEAgeRecognizer` · `DEPlaceRecognizer` · `DEClinicalIDRecognizer` · `DEOrganizationRecognizer` · `DEProfessionRecognizer` · `DEAnomalyRecognizer` (closed-vocab clinical anomaly → PERSON candidate) |

Most recognizers are language-gated (`SupportedLanguages()`), so a request with `language: "de"` skips US-specific recognizers and vice versa. Universal pattern recognizers (email, IBAN, etc.) run on every language.

## NER backends

- **GLiNER** (in-process, production default): open-set NER trained for PII; ~280 MB ONNX, ~200 ms/doc on a single amd64 vCPU. Wins leak rate vs Presidio, OpenAI Privacy Filter, and patterns-only across every benched corpus.
- **hugot/XLM-R** (in-process, legacy): pre-GLiNER backend, kept for regression detection. Slower and less recall than GLiNER on German clinical text.
- **Ollama** (local LLM): opt-in NER backend for users who already run an Ollama daemon. Pure-Go path (no CGO, no libonnxruntime).
- **Patterns-only** mode is fully supported (no model download, no CGO).

Both `GLiNER` and `hugot` require the `-tags hugot` build (CGO + libonnxruntime). See [DEPLOYMENT.md](DEPLOYMENT.md) for runtime requirements.

## Building a custom engine

```go
registry := analyzer.NewRecognizerRegistry()
registry.Add(recognizers.NewEmailRecognizer())
registry.Add(recognizers.NewCreditCardRecognizer())
// add your own; interface in analyzer/recognizer.go

engine := analyzer.NewAnalyzerEngine(registry)
```

## Writing a custom recognizer

Extensibility is the part Presidio gets right and we copied. A pattern
recognizer is a regex, a label, a language list, and an optional set of
context words that boost the score when they appear nearby. There are two
ways to add one.

### In-repo (joins every engine by default)

Drop the file into `analyzer/recognizers/` and add one line to `anonde.go`:

```go
// analyzer/recognizers/my_id.go
package recognizers

import "regexp"

var myIDRE = regexp.MustCompile(`\bMID-\d{6,10}\b`)

// NewMyIDRecognizer detects MY_ID entities.
func NewMyIDRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"MyIDRecognizer",
		[]string{"MY_ID"},
		[]string{"*"},                                // languages; "*" runs on all
		[]namedPattern{{re: myIDRE, score: 1.0}},
		[]string{"member id", "membership", "mid"},   // context boosts
	)
}
```

```go
// anonde.go — register inside patternRecognizers()
func patternRecognizers() []analyzer.EntityRecognizer {
	return []analyzer.EntityRecognizer{
		// Generic / international
		recognizers.NewEmailRecognizer(),
		recognizers.NewPhoneRecognizer(),
		// …
		recognizers.NewMyIDRecognizer(),   // ← add your recognizer here
	}
}
```

### As a library consumer (no fork)

Implement [`analyzer.EntityRecognizer`](../analyzer/recognizer.go) in your
own package and attach it to the engine's registry at startup — useful when
you `go get` anonde and don't want to maintain a fork:

```go
type MyIDRecognizer struct{ re *regexp.Regexp }

func (r *MyIDRecognizer) Name() string                 { return "MyIDRecognizer" }
func (r *MyIDRecognizer) SupportedEntities() []string  { return []string{"MY_ID"} }
func (r *MyIDRecognizer) SupportedLanguages() []string { return []string{"*"} }

func (r *MyIDRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var out []analyzer.RecognizerResult
	for _, m := range r.re.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 1.0,
			EntityType: "MY_ID", RecognizerName: "MyIDRecognizer",
		})
	}
	return out, nil
}

engine := anonde.DefaultAnalyzerEngine()
engine.Registry.Add(&MyIDRecognizer{re: regexp.MustCompile(`\bMID-\d{6,10}\b`)})
```

Either path joins the parallel-dispatch pipeline and the conflict resolver
handles overlap with existing recognizers automatically; NER preferences
(PERSON / ORG / LOC / AGE / PROFESSION / NRP) and the full pipeline rules
live in [ARCHITECTURE.md](ARCHITECTURE.md).

`SupportedLanguages()` returning `"*"` lets the recognizer match any language. NER-named recognizers (name suffix `NERRecognizer`) get auto-skipped under `DisableNER` and under the no-capitals heuristic.
