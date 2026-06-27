package anonymizer_test

import (
	"strings"
	"testing"

	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
)

// TestDetectOnlyTypesLeavesVerbatim verifies the type-level mark-only policy:
// a span whose EntityType is in DetectOnlyTypes is left verbatim and emits no
// AnonymizedItem, while other types still replace.
func TestDetectOnlyTypesLeavesVerbatim(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()

	text := "Visit https://x.io  Ada"
	urlStart := strings.Index(text, "https://x.io")
	personStart := strings.Index(text, "Ada")

	out, err := eng.Anonymize(text, results(
		r(urlStart, urlStart+len("https://x.io"), "URL", 0.99),
		r(personStart, personStart+len("Ada"), "PERSON", 0.99),
	), anonymizer.AnonymizerConfig{
		Operators:       anonymizer.OperatorMap{"*": &operators.Replace{}},
		DetectOnlyTypes: map[string]bool{"URL": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Text, "https://x.io") {
		t.Fatalf("expected URL left verbatim, got %q", out.Text)
	}
	if strings.Contains(out.Text, "Ada") || !strings.Contains(out.Text, "<PERSON>") {
		t.Fatalf("expected PERSON replaced, got %q", out.Text)
	}
	if len(out.Items) != 1 || out.Items[0].EntityType != "PERSON" {
		t.Fatalf("expected exactly one PERSON item, got %+v", out.Items)
	}
	for _, it := range out.Items {
		if it.EntityType == "URL" {
			t.Fatalf("URL must not appear in items (detect-only): %+v", it)
		}
	}
}

// TestDetectOnlyTypesMarkOnlySet models the agent's {URL, DATE_TIME, ID}
// mark-only set: a body of only those types comes back fully verbatim with no
// items, while the sensitive types remain replaceable.
func TestDetectOnlyTypesMarkOnlySet(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	text := "see https://x.io at 2026-06-22 ref 550e8400 ssn 123-45-6789"
	urlStart := strings.Index(text, "https://x.io")
	dateStart := strings.Index(text, "2026-06-22")
	idStart := strings.Index(text, "550e8400")
	ssnStart := strings.Index(text, "123-45-6789")

	out, err := eng.Anonymize(text, results(
		r(urlStart, urlStart+len("https://x.io"), "URL", 0.9),
		r(dateStart, dateStart+len("2026-06-22"), "DATE_TIME", 0.9),
		r(idStart, idStart+len("550e8400"), "ID", 0.9),
		r(ssnStart, ssnStart+len("123-45-6789"), "US_SSN", 0.9),
	), anonymizer.AnonymizerConfig{
		Operators:       anonymizer.OperatorMap{"*": &operators.Replace{}},
		DetectOnlyTypes: map[string]bool{"URL": true, "DATE_TIME": true, "ID": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.Text, "123-45-6789") || !strings.Contains(out.Text, "<US_SSN>") {
		t.Fatalf("expected US_SSN replaced (not mark-only), got %q", out.Text)
	}
	if !strings.Contains(out.Text, "https://x.io") || !strings.Contains(out.Text, "2026-06-22") || !strings.Contains(out.Text, "550e8400") {
		t.Fatalf("expected URL/DATE_TIME/ID verbatim, got %q", out.Text)
	}
	if len(out.Items) != 1 || out.Items[0].EntityType != "US_SSN" {
		t.Fatalf("expected only the US_SSN item, got %+v", out.Items)
	}
}

// TestAllowListLeavesVerbatim verifies the term-level allow policy: a span
// whose trimmed, case-insensitive surface is in AllowList is left verbatim,
// regardless of its entity type, while a non-allowed span of the same type
// still replaces.
func TestAllowListLeavesVerbatim(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	text := "Acme hired Bob"
	acmeStart := strings.Index(text, "Acme")
	bobStart := strings.Index(text, "Bob")

	out, err := eng.Anonymize(text, results(
		r(acmeStart, acmeStart+len("Acme"), "ORG", 0.99),
		r(bobStart, bobStart+len("Bob"), "PERSON", 0.99),
	), anonymizer.AnonymizerConfig{
		Operators: anonymizer.OperatorMap{"*": &operators.Replace{}},
		// Allow-term is lower-cased; the surface "Acme" must still match.
		AllowList: map[string]bool{"acme": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Text, "Acme") {
		t.Fatalf("expected allowed surface 'Acme' left verbatim, got %q", out.Text)
	}
	if strings.Contains(out.Text, "Bob") || !strings.Contains(out.Text, "<PERSON>") {
		t.Fatalf("expected PERSON 'Bob' replaced, got %q", out.Text)
	}
	if len(out.Items) != 1 || out.Items[0].EntityType != "PERSON" {
		t.Fatalf("expected only the PERSON item (allowed ORG excluded from reverse map), got %+v", out.Items)
	}
}

// TestDetectOnlyAndAllowRoundTrip confirms that replaced types remain
// deterministic and reversible even with both pass-through policies populated:
// a Replace round-trip is unaffected, and the kept spans contribute no items
// (so the reverse map the caller builds from Items excludes them).
func TestDetectOnlyAndAllowRoundTrip(t *testing.T) {
	eng := anonymizer.NewAnonymizerEngine()
	text := "ticket TCK-1 owner alice@x.io tag keepme"
	emStart := strings.Index(text, "alice@x.io")
	idStart := strings.Index(text, "TCK-1")
	allowStart := strings.Index(text, "keepme")

	out, err := eng.Anonymize(text, results(
		r(emStart, emStart+len("alice@x.io"), "EMAIL_ADDRESS", 0.99),
		r(idStart, idStart+len("TCK-1"), "ID", 0.9),
		r(allowStart, allowStart+len("keepme"), "CUSTOM", 0.9),
	), anonymizer.AnonymizerConfig{
		Operators:       anonymizer.OperatorMap{"*": &operators.Replace{NewValue: "<EMAIL>"}},
		DetectOnlyTypes: map[string]bool{"ID": true},
		AllowList:       map[string]bool{"keepme": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Text, "<EMAIL>") || strings.Contains(out.Text, "alice@x.io") {
		t.Fatalf("expected EMAIL replaced, got %q", out.Text)
	}
	if !strings.Contains(out.Text, "TCK-1") || !strings.Contains(out.Text, "keepme") {
		t.Fatalf("expected ID + allowed term verbatim, got %q", out.Text)
	}
	if len(out.Items) != 1 || out.Items[0].EntityType != "EMAIL_ADDRESS" {
		t.Fatalf("expected exactly one item (EMAIL only), got %+v", out.Items)
	}
}
