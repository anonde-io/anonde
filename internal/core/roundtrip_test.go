package core

import (
	"context"
	"strings"
	"testing"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/internal/metrics"
)

// Ingest → Reveal round-trip tests exercising the line-oriented content
// formats (NDJSON / Logs) and the per-request analyzer overrides
// (DisableNER, Entities allow-list). Uses the real DefaultAnalyzerEngine
// against the in-test Vault/Store stubs from testhelpers_test.go.

func newRoundtripService() *Service {
	return NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
		metrics.NewNoop(),
	)
}

func TestIngestReveal_NDJSON_RoundTrip(t *testing.T) {
	svc := newRoundtripService()
	in := `{"user":"John Doe","email":"john@example.com"}` + "\n" +
		`{"user":"Jane Roe","email":"jane@example.com"}` + "\n"

	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "ndjson-1",
		ContentFormat: "ndjson",
		Content:       in,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if strings.Contains(ing.AnonymizedContent, "john@example.com") || strings.Contains(ing.AnonymizedContent, "jane@example.com") {
		t.Fatalf("expected emails redacted, got %q", ing.AnonymizedContent)
	}
	if !strings.Contains(ing.AnonymizedContent, "\n") {
		t.Fatalf("expected line structure preserved, got %q", ing.AnonymizedContent)
	}

	rev, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID:      "acme",
		ID:            "ndjson-1",
		Actor:         "tester",
		Purpose:       "roundtrip",
		ContentFormat: "ndjson",
		Content:       ing.AnonymizedContent,
	})
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if !strings.Contains(rev.DeanonymizedContent, "john@example.com") {
		t.Fatalf("expected john@ restored, got %q", rev.DeanonymizedContent)
	}
	if !strings.Contains(rev.DeanonymizedContent, "jane@example.com") {
		t.Fatalf("expected jane@ restored, got %q", rev.DeanonymizedContent)
	}
}

func TestIngest_NDJSON_RejectsNonJSONLine(t *testing.T) {
	svc := newRoundtripService()
	in := `{"a":1}` + "\nnot json\n"
	_, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "ndjson-bad",
		ContentFormat: "ndjson",
		Content:       in,
	})
	if err == nil {
		t.Fatalf("expected ndjson to reject non-json line")
	}
}

func TestIngestReveal_Logs_MixedTextAndJSONWithANSI(t *testing.T) {
	svc := newRoundtripService()
	// Three lines: colored text log, JSON log, plain text; emails on each.
	in := "\x1b[31mERROR\x1b[0m contact alice@example.com about login\n" +
		`{"level":"info","email":"bob@example.com"}` + "\n" +
		"plain message charlie@example.com\n"

	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "logs-1",
		ContentFormat: "logs",
		Content:       in,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if strings.Contains(ing.AnonymizedContent, "alice@example.com") ||
		strings.Contains(ing.AnonymizedContent, "bob@example.com") ||
		strings.Contains(ing.AnonymizedContent, "charlie@example.com") {
		t.Fatalf("expected all emails redacted, got %q", ing.AnonymizedContent)
	}
	if strings.ContainsRune(ing.AnonymizedContent, 0x1b) {
		t.Fatalf("expected ANSI escapes stripped, got %q", ing.AnonymizedContent)
	}
	if strings.Count(ing.AnonymizedContent, "\n") != 3 {
		t.Fatalf("expected 3 newlines preserved, got %q", ing.AnonymizedContent)
	}

	rev, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID:      "acme",
		ID:            "logs-1",
		Actor:         "tester",
		Purpose:       "roundtrip",
		ContentFormat: "logs",
		Content:       ing.AnonymizedContent,
	})
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	for _, want := range []string{"alice@example.com", "bob@example.com", "charlie@example.com"} {
		if !strings.Contains(rev.DeanonymizedContent, want) {
			t.Fatalf("expected %q in revealed content, got %q", want, rev.DeanonymizedContent)
		}
	}
}

// TestIngest_DisableNER_SkipsModelBackedRecognizers verifies that DisableNER
// gates off only the model-backed NER recognizers (suffix "NERRecognizer",
// e.g. GLiNERRecognizer). Pattern-based PERSON detectors
// (ENAnomalyRecognizer, DEAnomalyRecognizer) keep firing; that's intentional
// and consistent across languages, and it's what makes patterns-only deploys
// usable for person redaction. Other pattern entities like EMAIL_ADDRESS
// remain unaffected.
func TestIngest_DisableNER_SkipsModelBackedRecognizers(t *testing.T) {
	svc := newRoundtripService()
	// "Patient John Doe …" triggers ENAnomalyRecognizer's structural path
	// ("Patient" + 1–4 capitalised tokens), which emits PERSON at score 0.85
	// well above the default 0.30 threshold. The bare-name path emits at
	// 0.25 and only clears the threshold via a context-keyword boost; using
	// the structural anchor here keeps the test stable regardless of the
	// surrounding text.
	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "ner-off",
		ContentFormat: "text",
		Content:       "Patient John Doe emailed alice@example.com",
		DisableNER:    true,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// Pattern-based ENAnomalyRecognizer still emits PERSON for the structural match.
	if strings.Contains(ing.AnonymizedContent, "John Doe") {
		t.Fatalf("expected pattern-based PERSON tokenization for %q, got %q", "John Doe", ing.AnonymizedContent)
	}
	if !strings.Contains(ing.AnonymizedContent, "<PERSON_") {
		t.Fatalf("expected PERSON token, got %q", ing.AnonymizedContent)
	}
	if strings.Contains(ing.AnonymizedContent, "alice@example.com") {
		t.Fatalf("expected email still redacted, got %q", ing.AnonymizedContent)
	}
}

func TestIngest_EntitiesAllowlist_OnlyEmail(t *testing.T) {
	svc := newRoundtripService()
	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "ents-1",
		ContentFormat: "text",
		Content:       "Email alice@example.com SSN 123-45-6789 IP 10.0.0.1",
		Entities:      []string{"EMAIL_ADDRESS"},
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	for _, ref := range ing.Tokens {
		if ref.EntityType != "EMAIL_ADDRESS" {
			t.Fatalf("expected only EMAIL_ADDRESS tokens, got %q", ref.EntityType)
		}
	}
	// SSN/IP should remain untouched.
	if !strings.Contains(ing.AnonymizedContent, "123-45-6789") {
		t.Fatalf("expected SSN preserved, got %q", ing.AnonymizedContent)
	}
	if !strings.Contains(ing.AnonymizedContent, "10.0.0.1") {
		t.Fatalf("expected IP preserved, got %q", ing.AnonymizedContent)
	}
}
