// Package anonde provides a Go implementation of the PII detection and
// anonymization pipeline inspired by Microsoft Presidio.
package anonde

import (
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/analyzer/recognizers"
	"github.com/moogacs/anonde/anonymizer"
)

// patternRecognizers returns the slice of non-NER recognizers shared by all engines.
func patternRecognizers() []analyzer.EntityRecognizer {
	return []analyzer.EntityRecognizer{
		recognizers.NewEmailRecognizer(),
		recognizers.NewPhoneRecognizer(),
		recognizers.NewCreditCardRecognizer(),
		recognizers.NewIBANRecognizer(),
		recognizers.NewIPAddressRecognizer(),
		recognizers.NewMACAddressRecognizer(),
		recognizers.NewURLRecognizer(),
		recognizers.NewCryptoRecognizer(),
		recognizers.NewDateTimeRecognizer(),
		recognizers.NewUSSocialSecurityRecognizer(),
		recognizers.NewUSPassportRecognizer(),
		recognizers.NewUSBankRecognizer(),
		recognizers.NewUSDriverLicenseRecognizer(),
		recognizers.NewUSITINRecognizer(),
		recognizers.NewMedicalLicenseRecognizer(),
	}
}

// DefaultAnalyzerEngine returns an AnalyzerEngine with prose-based NER (no model download required).
func DefaultAnalyzerEngine() *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewNERRecognizer())
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

// DefaultAnalyzerEngineWithPresidioRemote returns an engine that uses a remote
// Python Presidio Analyzer service for NER.
// endpoint defaults to "http://localhost:3000" if empty.
func DefaultAnalyzerEngineWithPresidioRemote(endpoint string) *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(recognizers.NewPresidioRemoteNERRecognizer(endpoint))
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnonymizerEngine returns a new AnonymizerEngine.
func DefaultAnonymizerEngine() *anonymizer.AnonymizerEngine {
	return anonymizer.NewAnonymizerEngine()
}
