package anonymizer_test

import (
	"strings"
	"testing"

	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
)

// TestKeepLeavesSpanVerbatim verifies the detect-but-don't-anonymize path:
// a span assigned operators.Keep is detected but left verbatim, while other
// types are still replaced, and NO AnonymizedItem is emitted for the kept span.
func TestKeepLeavesSpanVerbatim(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()

	// "Visit https://x.io  Ada"
	//        ^URL (6..18)        ^PERSON
	text := "Visit https://x.io  Ada"
	urlStart := strings.Index(text, "https://x.io")
	urlEnd := urlStart + len("https://x.io")
	personStart := strings.Index(text, "Ada")
	personEnd := personStart + len("Ada")

	out, err := eng.Anonymize(text, results(
		r(urlStart, urlEnd, "URL", 0.99),
		r(personStart, personEnd, "PERSON", 0.99),
	), anonymizer.Config(anonymizer.OperatorMap{
		"*":   &operators.Replace{},
		"URL": &operators.Keep{},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL must survive verbatim; PERSON must be replaced.
	if !strings.Contains(out.Text, "https://x.io") {
		t.Fatalf("expected URL left verbatim, got %q", out.Text)
	}
	if strings.Contains(out.Text, "Ada") {
		t.Fatalf("expected PERSON replaced, got %q", out.Text)
	}
	if !strings.Contains(out.Text, "<PERSON>") {
		t.Fatalf("expected <PERSON> token, got %q", out.Text)
	}

	// Exactly one item (the PERSON), none for the kept URL.
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 AnonymizedItem (PERSON only), got %d: %+v", len(out.Items), out.Items)
	}
	if out.Items[0].EntityType != "PERSON" {
		t.Fatalf("expected the sole item to be PERSON, got %q", out.Items[0].EntityType)
	}
	for _, it := range out.Items {
		if it.EntityType == "URL" {
			t.Fatalf("URL must not appear in items (it was detect-only): %+v", it)
		}
	}
}

// TestKeepMultipleTypesNoItems verifies that with several detect-only types
// configured, all are left verbatim and the items slice is empty, modeling
// the agent's {URL, DATE_TIME, ID} mark-only set on a body of only those types.
func TestKeepMultipleTypesNoItems(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()

	text := "see https://x.io at 2026-06-22 ref 550e8400"
	urlStart := strings.Index(text, "https://x.io")
	dateStart := strings.Index(text, "2026-06-22")
	idStart := strings.Index(text, "550e8400")

	out, err := eng.Anonymize(text, results(
		r(urlStart, urlStart+len("https://x.io"), "URL", 0.9),
		r(dateStart, dateStart+len("2026-06-22"), "DATE_TIME", 0.9),
		r(idStart, idStart+len("550e8400"), "ID", 0.9),
	), anonymizer.Config(anonymizer.OperatorMap{
		"*":         &operators.Replace{},
		"URL":       &operators.Keep{},
		"DATE_TIME": &operators.Keep{},
		"ID":        &operators.Keep{},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Text != text {
		t.Fatalf("expected fully verbatim output, got %q", out.Text)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected no AnonymizedItems for detect-only body, got %d: %+v", len(out.Items), out.Items)
	}
}

// TestKeepDoesNotAffectReplacedRoundTrip confirms the replaced types are
// unaffected by the presence of a Keep entry: a non-Keep operator still
// produces a deterministic, reversible replacement.
func TestKeepDoesNotAffectReplacedRoundTrip(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	text := "mail bob@x.io now"
	emStart := strings.Index(text, "bob@x.io")

	out, err := eng.Anonymize(text, results(
		r(emStart, emStart+len("bob@x.io"), "EMAIL_ADDRESS", 0.99),
	), anonymizer.Config(anonymizer.OperatorMap{
		"*":     &operators.Replace{NewValue: "<EMAIL>"},
		"URL":   &operators.Keep{},
		"EMAIL": &operators.Keep{}, // wrong key on purpose; EMAIL_ADDRESS must still replace
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Text, "<EMAIL>") || strings.Contains(out.Text, "bob@x.io") {
		t.Fatalf("expected EMAIL_ADDRESS replaced, got %q", out.Text)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
}

// TestKeepOperatorIsDetectOnly is a small unit guard on the marker.
func TestKeepOperatorIsDetectOnly(t *testing.T) {
	var op anonymizer.Operator = &operators.Keep{}
	do, ok := op.(anonymizer.DetectOnly)
	if !ok || !do.IsDetectOnly() {
		t.Fatal("operators.Keep must satisfy anonymizer.DetectOnly and report IsDetectOnly()==true")
	}
	got, err := (&operators.Keep{}).Anonymize("verbatim", "URL")
	if err != nil || got != "verbatim" {
		t.Fatalf("Keep.Anonymize defensive fallback should return text unchanged, got %q err %v", got, err)
	}
}
