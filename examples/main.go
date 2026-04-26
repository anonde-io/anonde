package main

import (
	"context"
	"fmt"
	"log"

	"github.com/moogacs/anonde"
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer"
	"github.com/moogacs/anonde/anonymizer/operators"
)

func main() {
	text := `Hi, I'm John. My email is john@example.com and my phone is +1-800-555-0199.
My SSN is 123-45-6789 and credit card 4111111111111111.
Visit us at https://example.com or reach via 192.168.1.1.
My Bitcoin wallet: 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2
IBAN: GB29NWBK60161331926819`

	analyzerEngine := anonde.DefaultAnalyzerEngine()
	anonymizerEngine := anonde.DefaultAnonymizerEngine()

	results, err := analyzerEngine.Analyze(context.Background(), text, analyzer.AnalysisConfig{
		Language:        "en",
		ScoreThreshold:  0.3,
		RemoveConflicts: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Detected entities ===")
	for _, r := range results {
		fmt.Printf("  [%d:%d] %-20s score=%.2f  %q\n",
			r.Start, r.End, r.EntityType, r.Score, text[r.Start:r.End])
	}

	cfg := anonymizer.AnonymizerConfig{
		"EMAIL_ADDRESS": &operators.Replace{NewValue: "<EMAIL>"},
		"PHONE_NUMBER":  &operators.Mask{CharsToMask: 4, FromEnd: true},
		"CREDIT_CARD":   &operators.Redact{},
		"US_SSN":        &operators.Hash{HashType: operators.HashSHA256},
		"CRYPTO":        &operators.Replace{},
		"IBAN_CODE":     &operators.Replace{},
		"IP_ADDRESS":    &operators.Replace{},
		"URL":           &operators.Replace{},
	}

	out, err := anonymizerEngine.Anonymize(text, results, cfg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n=== Anonymized text ===")
	fmt.Println(out.Text)

	fmt.Println("\n=== Anonymization log ===")
	for _, item := range out.Items {
		fmt.Printf("  [%d:%d] %-20s op=%-10s  %q\n",
			item.Start, item.End, item.EntityType, item.OperatorName, item.Text)
	}
}
