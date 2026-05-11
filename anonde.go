// Package anonde provides a Go implementation of the PII detection and
// anonymization pipeline inspired by Microsoft Presidio.
package anonde

import (
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/analyzer/recognizers"
	"github.com/moogacs/anonde/anonymizer"
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

		// United States
		recognizers.NewUSSocialSecurityRecognizer(),
		recognizers.NewUSPassportRecognizer(),
		recognizers.NewUSBankRecognizer(),
		recognizers.NewUSDriverLicenseRecognizer(),
		recognizers.NewUSITINRecognizer(),
		recognizers.NewMedicalLicenseRecognizer(),

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

// DefaultAnalyzerEngineWithHugot returns an engine that uses a pre-trained ONNX
// transformer model (via hugot) for NER.  Inference runs entirely in-process
// with no CGO or external service.
//
// modelsDir is the local directory where models are cached
// (defaults to ~/.cache/anonde/models if empty).
// modelName is the HuggingFace model ID
// (defaults to "dslim/bert-base-NER" if empty).
// autoDownload controls whether the model is fetched from HuggingFace Hub on
// first use when it is not already present locally.
func DefaultAnalyzerEngineWithHugot(modelsDir, modelName string, autoDownload bool) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    modelsDir,
		ModelName:    modelName,
		AutoDownload: autoDownload,
	}))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnonymizerEngine returns a new AnonymizerEngine.
func DefaultAnonymizerEngine() *anonymizer.AnonymizerEngine {
	return anonymizer.NewAnonymizerEngine()
}
