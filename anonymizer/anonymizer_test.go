package anonymizer_test

import (
	"strings"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
)

func results(items ...analyzer.RecognizerResult) []analyzer.RecognizerResult { return items }

func r(start, end int, entity string, score float64) analyzer.RecognizerResult {
	return analyzer.RecognizerResult{Start: start, End: end, EntityType: entity, Score: score}
}

func TestReplaceOperator(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	out, err := eng.Anonymize("hello world", results(r(6, 11, "WORD", 1.0)), anonymizer.Config(anonymizer.OperatorMap{
		"WORD": &operators.Replace{NewValue: "REPLACED"},
	}))
	if err != nil || out.Text != "hello REPLACED" {
		t.Fatalf("got %q, err %v", out.Text, err)
	}
}

func TestRedactOperator(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	out, _ := eng.Anonymize("foo secret bar", results(r(4, 10, "X", 1.0)), anonymizer.Config(anonymizer.OperatorMap{
		"X": &operators.Redact{},
	}))
	if out.Text != "foo  bar" {
		t.Fatalf("got %q", out.Text)
	}
}

func TestMaskOperator(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	out, _ := eng.Anonymize("4111111111111111", results(r(0, 16, "CC", 1.0)), anonymizer.Config(anonymizer.OperatorMap{
		"CC": &operators.Mask{CharsToMask: 12, FromEnd: true},
	}))
	if !strings.HasSuffix(out.Text, "****") || !strings.HasPrefix(out.Text, "4111") {
		t.Fatalf("got %q", out.Text)
	}
}

func TestHashOperator(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	out, _ := eng.Anonymize("secret", results(r(0, 6, "X", 1.0)), anonymizer.Config(anonymizer.OperatorMap{
		"X": &operators.Hash{HashType: operators.HashSHA256},
	}))
	if len(out.Text) != 64 {
		t.Fatalf("expected 64-char SHA256 hex, got %q", out.Text)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := []byte("0123456789abcdef") // 16-byte AES-128
	eng := anonymizer.NewAnonymizerEngine()
	out, err := eng.Anonymize("topsecret", results(r(0, 9, "X", 1.0)), anonymizer.Config(anonymizer.OperatorMap{
		"X": &operators.Encrypt{Key: key},
	}))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := operators.Decrypt(key, out.Text)
	if err != nil || plain != "topsecret" {
		t.Fatalf("decrypt failed: %v, got %q", err, plain)
	}
}

func TestDefaultOperatorFallback(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	out, _ := eng.Anonymize("hello world", results(r(6, 11, "UNKNOWN", 1.0)), anonymizer.AnonymizerConfig{})
	if out.Text != "hello <UNKNOWN>" {
		t.Fatalf("got %q", out.Text)
	}
}

func TestConflictResolution(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	// Two overlapping spans: higher score wins.
	res := results(
		r(0, 10, "A", 0.5),
		r(2, 8, "B", 0.9),
	)
	out, _ := eng.Anonymize("0123456789", res, anonymizer.Config(anonymizer.OperatorMap{
		"*": &operators.Replace{},
	}))
	if !strings.Contains(out.Text, "<B>") {
		t.Fatalf("expected B (higher score) to win, got %q", out.Text)
	}
}

func TestAnonymize_UnicodeWithByteOffsets(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	text := "Hi, I’m Sarah Connor"
	needle := "Sarah Connor"
	start := strings.Index(text, needle)
	if start < 0 {
		t.Fatalf("failed to locate %q in %q", needle, text)
	}
	end := start + len(needle)

	out, err := eng.Anonymize(text, results(r(start, end, "PERSON", 0.99)), anonymizer.Config(anonymizer.OperatorMap{
		"PERSON": &operators.Replace{NewValue: "<PERSON>"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Text != "Hi, I’m <PERSON>" {
		t.Fatalf("got %q", out.Text)
	}
}

func TestAnonymize_InvalidSpanReturnsError(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	_, err := eng.Anonymize("hello", results(r(4, 3, "X", 1.0)), anonymizer.Config(anonymizer.OperatorMap{
		"X": &operators.Replace{NewValue: "<X>"},
	}))
	if err == nil {
		t.Fatal("expected invalid span error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid recognizer span") {
		t.Fatalf("unexpected error: %v", err)
	}
}
