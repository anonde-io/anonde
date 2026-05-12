package benchmark_test

import (
	"context"
	"testing"

	"github.com/moogacs/anonde"
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer"
	"github.com/moogacs/anonde/anonymizer/operators"
)


// corpus covers all major PII types including NER entities (PERSON, LOCATION, ORGANIZATION).
var corpus = []string{
	// Mixed: pattern PII + NER entities
	`Hi, I'm Alice Johnson. My email is alice@example.com and I can be reached at +1-800-555-0199.
My SSN is 523-45-6789. Credit card: 4111111111111111. Visit https://example.com for more info.
Server IP: 192.168.1.100. Bitcoin wallet: 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2.
IBAN: GB29NWBK60161331926819. MAC: 00:1A:2B:3C:4D:5E. Born on 1990-03-15.`,

	// NER-heavy: persons, organizations, locations alongside pattern PII
	`Bob Smith, Senior Engineer at Microsoft, contacted support@company.org or called 020-7946-0958.
The meeting was held in Seattle at the Amazon headquarters on March 15, 2024.
Our server is at 10.0.0.1. Driver license: A1234567. Passport: A12345678.
Ethereum wallet: 0xde0B295669a9FD93d5F28D9Ec85E40f4cb697BAe.`,

	// Financial + NER
	`Sarah Connor from Goldman Sachs in New York sent payment to IBAN DE89370400440532013000.
Email her at sarah.connor@goldmansachs.com. Phone: (555) 867-5309.
Date of service: January 15, 2024. SSN: 234-56-7890.`,

	// Short with NER
	`Dr. James Wilson at Johns Hopkins emailed james@jhu.edu. Server IP: 172.16.0.1.`,

	// No PII — NER model still runs, exercises zero-result path
	`No PII entities in this plain text document about software engineering and cloud architecture.
The system processes millions of records per second and provides low latency responses.`,
}

var (
	analyzerEngine   *analyzer.AnalyzerEngine
	anonymizerEngine *anonymizer.AnonymizerEngine

	// analysisCfg runs the full pipeline including NER (PERSON/LOCATION/ORG).
	analysisCfg = analyzer.AnalysisConfig{
		Language:        "en",
		ScoreThreshold:  0.3,
		RemoveConflicts: true,
	}
	// analysisCfgPatternOnly skips NER for maximum throughput benchmarks.
	analysisCfgPatternOnly = analyzer.AnalysisConfig{
		Language:        "en",
		ScoreThreshold:  0.3,
		RemoveConflicts: true,
		DisableNER:      true,
	}
	anonCfg = anonymizer.AnonymizerConfig{
		"*": &operators.Replace{},
	}
)

func init() {
	anonymizerEngine = anonde.DefaultAnonymizerEngine()
	analyzerEngine = anonde.DefaultAnalyzerEngine()
}

// BenchmarkAnalyzeOnly measures PII detection throughput.
func BenchmarkAnalyzeOnly(b *testing.B) {
	for b.Loop() {
		for _, text := range corpus {
			_, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkAnalyzeAndAnonymize measures the full detect+anonymize pipeline.
func BenchmarkAnalyzeAndAnonymize(b *testing.B) {
	for b.Loop() {
		for _, text := range corpus {
			results, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
			if err != nil {
				b.Fatal(err)
			}
			_, err = anonymizerEngine.Anonymize(text, results, anonCfg)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkAnalyzeShort measures latency on a short single-entity text.
func BenchmarkAnalyzeShort(b *testing.B) {
	text := "Contact me at user@example.com for details."
	for b.Loop() {
		_, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAnalyzeLong measures throughput on a large text document.
func BenchmarkAnalyzeLong(b *testing.B) {
	text := ""
	for range 100 {
		text += corpus[0] + "\n"
	}
	for b.Loop() {
		_, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAnalyzeNoPII measures overhead when no PII is present.
func BenchmarkAnalyzeNoPII(b *testing.B) {
	text := corpus[4]
	for b.Loop() {
		_, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAnalyzeParallel measures concurrent throughput.
func BenchmarkAnalyzeParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, text := range corpus {
				_, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// ── Large data benchmarks — full pipeline (NER + patterns) ────────────────

func BenchmarkBulk_100KB(b *testing.B) {
	text := GenerateText(100*1024, 200)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulk_1MB(b *testing.B) {
	text := GenerateText(1024*1024, 200)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulk_10MB(b *testing.B) {
	text := GenerateText(10*1024*1024, 200)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulk_Batch100(b *testing.B) {
	docs := GenerateBatch(100, 1024)
	total := totalBytes(docs)
	b.SetBytes(int64(total))
	b.ResetTimer()
	for b.Loop() {
		for _, text := range docs {
			if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkBulk_Batch1000(b *testing.B) {
	docs := GenerateBatch(1000, 1024)
	total := totalBytes(docs)
	b.SetBytes(int64(total))
	b.ResetTimer()
	for b.Loop() {
		for _, text := range docs {
			if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkBulk_Batch1000Parallel(b *testing.B) {
	docs := GenerateBatch(1000, 1024)
	b.SetBytes(int64(totalBytes(docs)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, text := range docs {
				if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// ── Large data benchmarks — pattern-only (DisableNER) ─────────────────────

func BenchmarkBulkPatternOnly_100KB(b *testing.B) {
	text := GenerateText(100*1024, 200)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfgPatternOnly); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkPatternOnly_1MB(b *testing.B) {
	text := GenerateText(1024*1024, 200)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfgPatternOnly); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkPatternOnly_Batch1000(b *testing.B) {
	docs := GenerateBatch(1000, 1024)
	b.SetBytes(int64(totalBytes(docs)))
	b.ResetTimer()
	for b.Loop() {
		for _, text := range docs {
			if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfgPatternOnly); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkBulkPatternOnly_Batch1000Parallel(b *testing.B) {
	docs := GenerateBatch(1000, 1024)
	b.SetBytes(int64(totalBytes(docs)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, text := range docs {
				if _, err := analyzerEngine.Analyze(context.Background(), text, analysisCfgPatternOnly); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func totalBytes(docs []string) int {
	n := 0
	for _, d := range docs {
		n += len(d)
	}
	return n
}
