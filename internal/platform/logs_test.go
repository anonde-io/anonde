package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moogacs/anonde"
)

func newTestService() *Service {
	return NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		NewMemoryVault(),
		NewMemoryStore(),
		allowAllPolicy{},
	)
}

// ---------------------------------------------------------------------------
// content format helpers
// ---------------------------------------------------------------------------

func TestNormalizeContentFormat_NewFormats(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"ndjson":     contentFormatNDJSON,
		"NDJSON":     contentFormatNDJSON,
		"jsonl":      contentFormatNDJSON,
		"json-lines": contentFormatNDJSON,
		"logs":       contentFormatLogs,
		"log":        contentFormatLogs,
	}
	for in, want := range cases {
		if got := normalizeContentFormat(in); got != want {
			t.Errorf("normalizeContentFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveAutoContentFormat_NDJSON(t *testing.T) {
	t.Parallel()
	in := `{"a":1}` + "\n" + `{"b":2}` + "\n"
	if got := resolveAutoContentFormat(in); got != contentFormatNDJSON {
		t.Errorf("expected ndjson, got %q", got)
	}
}

func TestStripANSI_RemovesEscapes(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mERROR\x1b[0m something happened"
	got := stripANSI(in)
	if got != "ERROR something happened" {
		t.Errorf("expected escapes removed, got %q", got)
	}
}

func TestSanitizeUTF8_ReplacesInvalid(t *testing.T) {
	t.Parallel()
	// "abc" + invalid byte 0xff + "def"
	in := "abc\xffdef"
	got := sanitizeUTF8(in)
	if !strings.Contains(got, "abc") || !strings.Contains(got, "def") {
		t.Errorf("expected valid surrounding text preserved, got %q", got)
	}
	if strings.ContainsRune(got, 0xff) {
		t.Errorf("expected invalid byte to be removed, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// NDJSON ingest/reveal round trip
// ---------------------------------------------------------------------------

func TestIngestReveal_NDJSON_RoundTrip(t *testing.T) {
	svc := newTestService()
	in := `{"user":"John Doe","email":"john@example.com"}` + "\n" +
		`{"user":"Jane Roe","email":"jane@example.com"}` + "\n"

	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		DocID:         "ndjson-1",
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
		DocID:         "ndjson-1",
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
	svc := newTestService()
	in := `{"a":1}` + "\nnot json\n"
	_, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		DocID:         "ndjson-bad",
		ContentFormat: "ndjson",
		Content:       in,
	})
	if err == nil {
		t.Fatalf("expected ndjson to reject non-json line")
	}
}

// ---------------------------------------------------------------------------
// Logs format: per-line auto, ANSI stripping, mixed text/JSON
// ---------------------------------------------------------------------------

func TestIngestReveal_Logs_MixedTextAndJSONWithANSI(t *testing.T) {
	svc := newTestService()
	// Three lines: colored text log, JSON log, plain text — emails on each.
	in := "\x1b[31mERROR\x1b[0m contact alice@example.com about login\n" +
		`{"level":"info","email":"bob@example.com"}` + "\n" +
		"plain message charlie@example.com\n"

	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		DocID:         "logs-1",
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
		DocID:         "logs-1",
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

// ---------------------------------------------------------------------------
// Per-request analyzer overrides: DisableNER + Entities
// ---------------------------------------------------------------------------

func TestIngest_DisableNER_KeepsPersonInClear(t *testing.T) {
	svc := newTestService()
	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		DocID:         "ner-off",
		ContentFormat: "text",
		Content:       "John Doe emailed alice@example.com",
		DisableNER:    true,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// PERSON should remain (NER off); EMAIL_ADDRESS should still be redacted.
	if !strings.Contains(ing.AnonymizedContent, "John Doe") {
		t.Fatalf("expected name preserved with DisableNER, got %q", ing.AnonymizedContent)
	}
	if strings.Contains(ing.AnonymizedContent, "alice@example.com") {
		t.Fatalf("expected email still redacted, got %q", ing.AnonymizedContent)
	}
}

func TestIngest_EntitiesAllowlist_OnlyEmail(t *testing.T) {
	svc := newTestService()
	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		DocID:         "ents-1",
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

// Tenant-scoped token reuse across documents was deliberately removed (see
// TODO.md). Tokens are minted per-doc via a per-tenant counter; the same
// cleartext in two docs gets two distinct tokens.

// ---------------------------------------------------------------------------
// Single-pass reveal replacer: long token must not be shadowed by short one
// ---------------------------------------------------------------------------

func TestBuildTokenReplacer_PrefersLongerMatch(t *testing.T) {
	t.Parallel()
	tokens := []string{
		"<EMAIL_ADDRESS_ACME_000001>",
		"<EMAIL_ADDRESS_ACME_000001_X>", // longer, must win on overlap
	}
	resolved := map[string]string{
		"<EMAIL_ADDRESS_ACME_000001>":   "alice@example.com",
		"<EMAIL_ADDRESS_ACME_000001_X>": "alice-extended@example.com",
	}
	replace, err := buildTokenReplacer(tokens, resolved)
	if err != nil {
		t.Fatalf("build replacer: %v", err)
	}
	in := "see <EMAIL_ADDRESS_ACME_000001_X> and <EMAIL_ADDRESS_ACME_000001>"
	out := replace(in)
	if !strings.Contains(out, "alice-extended@example.com") {
		t.Fatalf("expected longer token resolved, got %q", out)
	}
	if !strings.Contains(out, "alice@example.com") {
		t.Fatalf("expected shorter token resolved, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// HTTP body size limit
// ---------------------------------------------------------------------------

func TestHTTP_IngestRejectsOversizedBody(t *testing.T) {
	svc := newTestService()
	api := NewHTTPServer(svc)
	api.SetMaxRequestBytes(64) // tiny

	srv := httptest.NewServer(api.Routes())
	defer srv.Close()

	body := `{"tenant_id":"acme","doc_id":"d","content_format":"text","content":"` +
		strings.Repeat("a", 1024) + `"}`
	resp, err := http.Post(srv.URL+"/v1/ingest", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", resp.StatusCode)
	}
}
