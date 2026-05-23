// Package anonde provides a Go implementation of the PII detection and
// anonymization pipeline inspired by Microsoft Presidio.
package anonde

import (
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/auditor"
	"github.com/anonde-io/anonde/analyzer/recognizers"
	"github.com/anonde-io/anonde/analyzer/reconciler"
	"github.com/anonde-io/anonde/anonymizer"
)

// patternRecognizers returns the slice of non-NER recognizers shared by all engines.
//
// Recognizers are grouped by region. Adding a new region: append below the
// existing block and add a one-line comment delimiter.
func patternRecognizers() []analyzer.EntityRecognizer {
	return []analyzer.EntityRecognizer{
		// Generic / international
		recognizers.NewEmailRecognizer(),
		recognizers.NewPhoneRecognizer(),
		recognizers.NewCreditCardRecognizer(),
		recognizers.NewIBANRecognizer(),
		recognizers.NewIPAddressRecognizer(),
		recognizers.NewMACAddressRecognizer(),
		recognizers.NewURLRecognizer(),
		recognizers.NewCryptoRecognizer(),
		recognizers.NewDateTimeRecognizer(),

		// United States / English-language clinical
		recognizers.NewUSSocialSecurityRecognizer(),
		recognizers.NewUSPassportRecognizer(),
		recognizers.NewUSBankRecognizer(),
		recognizers.NewUSDriverLicenseRecognizer(),
		recognizers.NewUSITINRecognizer(),
		recognizers.NewUSZipRecognizer(),
		recognizers.NewMedicalLicenseRecognizer(),
		recognizers.NewENAnomalyRecognizer(),
		recognizers.NewENOrganizationRecognizer(),
		recognizers.NewENStreetRecognizer(),
		recognizers.NewENPersonRecognizer(),

		// Cross-language structured PII
		recognizers.NewSocialHandleRecognizer(),

		// United Kingdom
		recognizers.NewUKNHSRecognizer(),
		recognizers.NewUKNINORecognizer(),

		// Italy
		recognizers.NewITFiscalCodeRecognizer(),
		recognizers.NewITDriverLicenseRecognizer(),
		recognizers.NewITVATCodeRecognizer(),
		recognizers.NewITPassportRecognizer(),
		recognizers.NewITIdentityCardRecognizer(),

		// Spain
		recognizers.NewESNIFRecognizer(),
		recognizers.NewESNIERecognizer(),

		// Australia
		recognizers.NewAUABNRecognizer(),
		recognizers.NewAUACNRecognizer(),
		recognizers.NewAUTFNRecognizer(),
		recognizers.NewAUMedicareRecognizer(),

		// India
		recognizers.NewINPANRecognizer(),
		recognizers.NewINAadhaarRecognizer(),
		recognizers.NewINVehicleRegistrationRecognizer(),
		recognizers.NewINVoterRecognizer(),
		recognizers.NewINPassportRecognizer(),

		// Poland
		recognizers.NewPLPESELRecognizer(),

		// Singapore
		recognizers.NewSGNRICRecognizer(),
		recognizers.NewSGUENRecognizer(),

		// Finland
		recognizers.NewFIPersonalIdentityCodeRecognizer(),

		// Korea
		recognizers.NewKRRRNRecognizer(),

		// Germany
		recognizers.NewDEDateTimeRecognizer(),
		recognizers.NewDEDateContextRecognizer(),
		recognizers.NewDEPhoneRecognizer(),
		recognizers.NewDEPostalCodeRecognizer(),
		recognizers.NewDEStreetRecognizer(),
		recognizers.NewDESteuerIDRecognizer(),
		recognizers.NewDEAgeRecognizer(),
		recognizers.NewDEPlaceRecognizer(),
		recognizers.NewDEClinicalIDRecognizer(),
		recognizers.NewDEOrganizationRecognizer(),
		recognizers.NewDELegalFinanceOrgRecognizer(),
		recognizers.NewBICRecognizer(),
		recognizers.NewDEProfessionRecognizer(),
		recognizers.NewDEAnomalyRecognizer(),
	}
}

// DefaultAnalyzerEngine returns an AnalyzerEngine with all pattern recognizers
// and **no** NER. This is the zero-dependency, no-model-download path —
// fastest, smallest binary footprint, but PERSON / LOCATION / ORGANIZATION
// will not be detected.
//
// For NER, use DefaultAnalyzerEngineWithHugot (in-process ONNX, recommended)
// or DefaultAnalyzerEngineWithOllama.
func DefaultAnalyzerEngine() *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithOllama returns an engine that uses a local Ollama instance for NER.
// All inference runs locally — no data leaves the machine.
// endpoint defaults to "http://localhost:11434" if empty; model defaults to "phi3:mini" if empty.
func DefaultAnalyzerEngineWithOllama(endpoint, model string) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewOllamaNERRecognizer(endpoint, model))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithHugot returns an engine that uses a pre-trained
// ONNX transformer model (via hugot) for NER.
//
// Build-tagged: the real implementation lives in hugot_on.go and only
// compiles with `-tags hugot`. The default build uses hugot_off.go, which
// log.Fatalfs on call. This keeps the hugot transitive dependency graph
// (onnxruntime-go, tokenizers, …) out of patterns-only and Ollama-only
// builds.

// WithOllamaReconciler attaches a local-Ollama LLM reconciler to the given
// engine. The reconciler gates an LLM call on borderline-confidence
// candidates (score in [LowGate, HighGate)) and drops those the model
// classifies as false positives.
//
// Returns the same engine for chaining. Pass a zero OllamaConfig for
// production defaults (llama3.2:3b, endpoint localhost:11434, 4 workers,
// 5 s per-span timeout).
//
// Fail-open: on any LLM error the reconciler keeps the candidate, so
// attaching it CANNOT raise the leak rate vs not attaching it.
func WithOllamaReconciler(e *analyzer.AnalyzerEngine, cfg reconciler.OllamaConfig) *analyzer.AnalyzerEngine {
	e.Reconciler = reconciler.NewOllama(cfg)
	return e
}

// WithOllamaAuditor attaches a local-Ollama LLM final-audit-pass to the
// given engine. After all other recognizers run, the auditor reviews the
// document for PII the rest of the pipeline missed and appends its
// findings.
//
// Fails open: on any LLM error the auditor returns nothing, so attaching
// it CANNOT raise leak rate vs. not attaching it.
//
// Use a 7B+ instruction-following model for usable quality on German
// clinical text; smaller models produce unreliable JSON. Pass a zero
// OllamaAuditorConfig for defaults (llama3.1:8b, 60s timeout).
func WithOllamaAuditor(e *analyzer.AnalyzerEngine, cfg auditor.OllamaConfig) *analyzer.AnalyzerEngine {
	e.Auditor = auditor.NewOllama(cfg)
	return e
}

// DefaultAnonymizerEngine returns a new AnonymizerEngine.
func DefaultAnonymizerEngine() *anonymizer.AnonymizerEngine {
	return anonymizer.NewAnonymizerEngine()
}
