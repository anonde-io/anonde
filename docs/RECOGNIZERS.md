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

```go
type MyRecognizer struct{}

func (r *MyRecognizer) Name() string                 { return "MyRecognizer" }
func (r *MyRecognizer) SupportedEntities() []string  { return []string{"MY_ENTITY"} }
func (r *MyRecognizer) SupportedLanguages() []string { return []string{"en", "*"} }

func (r *MyRecognizer) Analyze(ctx context.Context, text string, entities []string, lang string) ([]analyzer.RecognizerResult, error) {
	return []analyzer.RecognizerResult{
		{Start: 0, End: 5, Score: 0.9, EntityType: "MY_ENTITY", RecognizerName: r.Name()},
	}, nil
}
```

`SupportedLanguages()` returning `"*"` lets the recognizer match any language. NER-named recognizers (name suffix `NERRecognizer`) get auto-skipped under `DisableNER` and under the no-capitals heuristic.
