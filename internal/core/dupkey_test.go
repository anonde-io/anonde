package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/internal/metrics"
)

// Duplicate JSON keys are last-wins. The offset-aware content walker used by
// Ingest/Synthesize visits every occurrence while parsing, but the retained
// (final) value alone may reach the side-effecting callback — otherwise a
// shadowed PII value that never appears in the emitted document still mints a
// vault entry and shows up in findings/tokens. These tests pin that only the
// retained value is reported, stored, and offset-located.

// dupKeyJSON shadows an email under key "a" with a second email; "alice" is a
// substring that appears ONLY inside the shadowed (dropped) value, so any
// finding / token / vault entry mentioning it is a leak of the shadow.
const dupKeyJSON = `{"a":"alice@example.com","a":"bob@example.com"}`

func newInspectableService() (*Service, *testVault, *testStore) {
	vault := newTestVault()
	store := newTestStore()
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		vault,
		store,
		allowAllPolicy{},
		metrics.NewNoop(),
	)
	return svc, vault, store
}

// emailFindings returns the EMAIL_ADDRESS findings, asserting each slices back
// to a real address in doc and that none lands inside the shadowed value.
func emailFindings(t *testing.T, doc string, findings []analyzer.RecognizerResult) []analyzer.RecognizerResult {
	t.Helper()
	var emails []analyzer.RecognizerResult
	for _, f := range findings {
		if f.Start < 0 || f.End > len(doc) || f.Start > f.End {
			t.Fatalf("finding span out of bounds: start=%d end=%d doclen=%d", f.Start, f.End, len(doc))
		}
		if strings.Contains(doc[f.Start:f.End], "alice") {
			t.Fatalf("finding sliced into the shadowed value: %d:%d = %q", f.Start, f.End, doc[f.Start:f.End])
		}
		if f.EntityType == "EMAIL_ADDRESS" {
			emails = append(emails, f)
		}
	}
	return emails
}

func TestIngest_JSON_DuplicateKeyShadowedValueNoSideEffects(t *testing.T) {
	svc, vault, _ := newInspectableService()
	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "dupkey-ingest",
		ContentFormat: "json",
		Content:       dupKeyJSON,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// (a) exactly one EMAIL finding, for the retained value only.
	emails := emailFindings(t, dupKeyJSON, ing.Findings)
	if len(emails) != 1 {
		t.Fatalf("expected exactly one EMAIL finding (retained only), got %d: %+v", len(emails), emails)
	}
	// (d) the retained value's offset slices back to the retained occurrence.
	if got := dupKeyJSON[emails[0].Start:emails[0].End]; got != "bob@example.com" {
		t.Fatalf("retained finding must slice to bob@example.com, got %q", got)
	}

	// (a) exactly one EMAIL token, for the retained value only.
	var emailTokens int
	for _, tok := range ing.Tokens {
		if strings.Contains(dupKeyJSON[tok.Start:tok.End], "alice") {
			t.Fatalf("token sliced into the shadowed value: %d:%d = %q", tok.Start, tok.End, dupKeyJSON[tok.Start:tok.End])
		}
		if tok.EntityType == "EMAIL_ADDRESS" {
			emailTokens++
			if got := dupKeyJSON[tok.Start:tok.End]; got != "bob@example.com" {
				t.Fatalf("retained token must slice to bob@example.com, got %q", got)
			}
		}
	}
	if emailTokens != 1 {
		t.Fatalf("expected exactly one EMAIL token (retained only), got %d", emailTokens)
	}

	// (b) no vault entry for the shadowed value.
	vault.mu.Lock()
	for _, e := range vault.m {
		if strings.Contains(e.Cleartext, "alice") {
			t.Fatalf("vault leaked shadowed value: %+v", e)
		}
	}
	vault.mu.Unlock()

	// The emitted document is last-wins and never mentions either raw address.
	if strings.Contains(ing.AnonymizedContent, "alice@example.com") {
		t.Fatalf("shadowed value leaked into output: %q", ing.AnonymizedContent)
	}
	if strings.Contains(ing.AnonymizedContent, "bob@example.com") {
		t.Fatalf("retained value not anonymized in output: %q", ing.AnonymizedContent)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(ing.AnonymizedContent), &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, ing.AnonymizedContent)
	}
	if len(obj) != 1 {
		t.Fatalf("expected a single last-wins key, got %d: %v", len(obj), obj)
	}
}

func TestSynthesize_JSON_DuplicateKeyShadowedValueNoSideEffects(t *testing.T) {
	svc, _, _ := newInspectableService()
	resp, err := svc.Synthesize(context.Background(), SynthesizeRequest{
		ContentFormat: "json",
		Content:       dupKeyJSON,
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	// (a) exactly one EMAIL finding, for the retained value only.
	emails := emailFindings(t, dupKeyJSON, resp.Findings)
	if len(emails) != 1 {
		t.Fatalf("expected exactly one EMAIL finding (retained only), got %d: %+v", len(emails), emails)
	}
	// (d) offset slices back to the retained occurrence.
	if got := dupKeyJSON[emails[0].Start:emails[0].End]; got != "bob@example.com" {
		t.Fatalf("retained finding must slice to bob@example.com, got %q", got)
	}

	// Neither raw address survives, and the shadowed one never appears.
	if strings.Contains(resp.Content, "alice@example.com") {
		t.Fatalf("shadowed value leaked into synthesized output: %q", resp.Content)
	}
	if strings.Contains(resp.Content, "bob@example.com") {
		t.Fatalf("retained value not synthesized in output: %q", resp.Content)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, resp.Content)
	}
	if len(obj) != 1 {
		t.Fatalf("expected a single last-wins key, got %d: %v", len(obj), obj)
	}
}
