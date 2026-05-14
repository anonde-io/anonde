package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anonde-io/anonde"
)

type allowAllPolicy struct{}

func (allowAllPolicy) AllowDetokenize(context.Context, DetokenizeRequest) error { return nil }

func TestReveal_NoTokensReturnsInputContent(t *testing.T) {
	svc := NewService(
		nil,
		nil,
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
	)

	const tenantID = "tenant-a"
	const docID = "doc-1"
	const content = "Hello world with no tokens"

	if err := svc.store.Put(context.Background(), StoreRecord{
		TenantID:          tenantID,
		ID:             docID,
		AnonymizedContent: content,
		Tokens:            nil,
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	out, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID: tenantID,
		ID:    docID,
		Actor:    "tester",
		Purpose:  "debug",
		Content:  content,
	})
	if err != nil {
		t.Fatalf("unexpected reveal error: %v", err)
	}
	if out.DeanonymizedContent != content {
		t.Fatalf("expected unchanged content, got %q", out.DeanonymizedContent)
	}
	if len(out.Resolved) != 0 {
		t.Fatalf("expected no resolved tokens, got %d", len(out.Resolved))
	}
}

func TestIngestReveal_JSONContent(t *testing.T) {
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
	)

	ingestResp, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "tenant-json",
		ID:         "doc-json-1",
		ContentFormat: "json",
		Content:       `{"user":"John Doe","email":"john@example.com","nested":{"note":"call +1-800-555-0199"}}`,
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if !strings.Contains(ingestResp.AnonymizedContent, "<EMAIL_ADDRESS_") {
		t.Fatalf("expected anonymized email token, got %q", ingestResp.AnonymizedContent)
	}

	revealResp, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID:      "tenant-json",
		ID:         "doc-json-1",
		Actor:         "tester",
		Purpose:       "verification",
		ContentFormat: "json",
		Content:       ingestResp.AnonymizedContent,
	})
	if err != nil {
		t.Fatalf("reveal failed: %v", err)
	}
	if !strings.Contains(revealResp.DeanonymizedContent, "john@example.com") {
		t.Fatalf("expected deanonymized email, got %q", revealResp.DeanonymizedContent)
	}
}

func TestIngestReveal_JSONFixtureRoundTrip(t *testing.T) {
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
	)

	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "testdata", "pii-sample.json"))
	if err != nil {
		t.Fatalf("read json fixture: %v", err)
	}
	original := string(raw)

	ingestResp, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:         "doc-json-fixture-1",
		ContentFormat: "json",
		Content:       original,
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if len(ingestResp.Tokens) == 0 {
		t.Fatalf("expected at least one token from fixture")
	}
	if strings.Contains(ingestResp.AnonymizedContent, "john.doe@example.com") {
		t.Fatalf("expected email to be anonymized, got %q", ingestResp.AnonymizedContent)
	}

	revealResp, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID:      "acme",
		ID:         "doc-json-fixture-1",
		Actor:         "tester",
		Purpose:       "roundtrip-check",
		ContentFormat: "json",
		Content:       ingestResp.AnonymizedContent,
	})
	if err != nil {
		t.Fatalf("reveal failed: %v", err)
	}

	var originalJSON any
	if err := json.Unmarshal([]byte(original), &originalJSON); err != nil {
		t.Fatalf("parse original fixture json: %v", err)
	}
	var revealedJSON any
	if err := json.Unmarshal([]byte(revealResp.DeanonymizedContent), &revealedJSON); err != nil {
		t.Fatalf("parse revealed json: %v", err)
	}
	if !reflect.DeepEqual(originalJSON, revealedJSON) {
		t.Fatalf("expected reveal output to match original json")
	}
}

func TestIngest_AutoDetectsJSON(t *testing.T) {
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
	)

	in := `{"email":"john@example.com","note":"SSN 123-45-6789"}`
	out, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:         "doc-auto-json",
		ContentFormat: "auto",
		Content:       in,
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.AnonymizedContent), "{") {
		t.Fatalf("expected json-shaped anonymized content, got %q", out.AnonymizedContent)
	}
	if strings.Contains(out.AnonymizedContent, "john@example.com") {
		t.Fatalf("expected email to be anonymized")
	}
}

func TestIngest_AutoDetectsText(t *testing.T) {
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
	)

	in := "Contact john@example.com"
	out, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:         "doc-auto-text",
		ContentFormat: "auto",
		Content:       in,
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(out.AnonymizedContent), "{") {
		t.Fatalf("expected text anonymized content, got %q", out.AnonymizedContent)
	}
	if strings.Contains(out.AnonymizedContent, "john@example.com") {
		t.Fatalf("expected email to be anonymized")
	}
}

func TestIngestReveal_AutoWithMixedTextAndJSONSnippet(t *testing.T) {
	svc := NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		newTestVault(),
		newTestStore(),
		allowAllPolicy{},
	)

	// Mixed payload in one message: free text plus an embedded JSON-looking snippet.
	// This is not valid top-level JSON, so auto mode should treat it as text.
	// Uses pattern-detectable PII only — DefaultAnalyzerEngine ships without
	// NER, so PERSON detection is intentionally not exercised here.
	input := `Please review this payload {"email":"john@example.com","phone":"+1-800-555-0199"} for processing.`

	ingestResp, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:         "doc-auto-mixed",
		ContentFormat: "auto",
		Content:       input,
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if len(ingestResp.Tokens) == 0 {
		t.Fatalf("expected tokens for mixed payload")
	}
	if strings.Contains(ingestResp.AnonymizedContent, "john@example.com") {
		t.Fatalf("expected email to be anonymized")
	}
	if strings.Contains(ingestResp.AnonymizedContent, "+1-800-555-0199") {
		t.Fatalf("expected phone to be anonymized")
	}

	revealResp, err := svc.Reveal(context.Background(), RevealRequest{
		TenantID:      "acme",
		ID:         "doc-auto-mixed",
		Actor:         "tester",
		Purpose:       "roundtrip-check",
		ContentFormat: "auto",
		Content:       ingestResp.AnonymizedContent,
	})
	if err != nil {
		t.Fatalf("reveal failed: %v", err)
	}
	if revealResp.DeanonymizedContent != input {
		t.Fatalf("expected roundtrip content match")
	}
}
