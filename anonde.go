// Package anonde provides a Go implementation of the PII detection and
// anonymization pipeline inspired by Microsoft Presidio.
package anonde

import (
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
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
		recognizers.NewSecretRecognizer(),
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
		recognizers.NewUsernameRecognizer(),

		// Romance-language streets (Italian / French / Spanish). German
		// streets live with the DE block below; English under the US/EN
		// block above.
		recognizers.NewFRStreetRecognizer(),
		recognizers.NewITStreetRecognizer(),
		recognizers.NewESStreetRecognizer(),

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

		// Romania.
		// Money values intentionally NOT included; user policy says
		// monetary amounts (fine values, line-item amounts on
		// receipts/invoices/garnishment notices) are not PII. The
		// Romanian MoneyRecognizer is still defined in ro_patterns.go
		// for callers who want it, but it's omitted from the default
		// kernel.
		recognizers.NewRomanianPhoneRecognizer(),
		recognizers.NewRomanianCNPRecognizer(),
		recognizers.NewRomanianVehicleRegRecognizer(),
		recognizers.NewRomanianTreasuryAccountRecognizer(),
		recognizers.NewRomanianStreetRecognizer(),
		recognizers.NewRomanianObservationRecognizer(),
		recognizers.NewTimeOfDayRecognizer(),

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
		recognizers.NewISINRecognizer(),
		recognizers.NewDEProfessionRecognizer(),
		recognizers.NewDEAnomalyRecognizer(),
	}
}

// DefaultAnalyzerEngine returns an AnalyzerEngine with all pattern recognizers
// and **no** NER. This is the zero-dependency, no-model-download path,
// fastest, smallest binary footprint, but PERSON / LOCATION / ORGANIZATION
// will not be detected.
//
// For NER, use DefaultAnalyzerEngineWithGLiNERConfig (in-process ONNX,
// recommended).
func DefaultAnalyzerEngine() *analyzer.AnalyzerEngine {
	registry := analyzer.NewRecognizerRegistry()
	registry.Add(patternRecognizers()...)
	return analyzer.NewAnalyzerEngine(registry)
}

// DefaultAnalyzerEngineWithGLiNERConfig wires a Go-native GLiNER recognizer
// for in-process open-set NER.
//
// Build-tagged: the real implementation lives in ner_on.go and only
// compiles with `-tags ner`. The default build uses ner_off.go, which
// log.Fatalfs on call. This keeps the GLiNER transitive dependency graph
// (onnxruntime-go, tokenizers, …) out of patterns-only builds.

// DefaultAnonymizerEngine returns a new AnonymizerEngine.
func DefaultAnonymizerEngine() *anonymizer.AnonymizerEngine {
	return anonymizer.NewAnonymizerEngine()
}
