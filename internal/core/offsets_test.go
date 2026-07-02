package core

import (
	"context"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
)

// These tests pin the API contract that TokenRef.Start/End and Finding
// offsets returned by Ingest and Synthesize are DOCUMENT-relative: a byte
// position in the submitted document, not local to the JSON string leaf or
// the NDJSON/log line the PII was found in. The strong check is that slicing
// the submitted content at each returned [Start:End] reproduces the exact
// PII, and that a token in a later leaf / later line reports an offset past
// an earlier one (never a duplicate leaf-local 0).

const twoLeafJSON = `{"a":"contact alice@example.com","b":"then bob@example.com"}`

const twoLineNDJSON = `{"email":"alice@example.com"}` + "\n" +
	`{"email":"bob@example.com"}` + "\n"

// emailOffsets returns the document-relative Start offsets of the alice/bob
// EMAIL_ADDRESS findings, verifying each returned span slices back to the
// exact address in doc.
func emailOffsets(t *testing.T, doc string, findings []analyzer.RecognizerResult) (alice, bob int) {
	t.Helper()
	alice, bob = -1, -1
	for _, f := range findings {
		if f.EntityType != "EMAIL_ADDRESS" {
			continue
		}
		if f.Start < 0 || f.End > len(doc) || f.Start > f.End {
			t.Fatalf("finding span out of document bounds: start=%d end=%d doclen=%d", f.Start, f.End, len(doc))
		}
		switch doc[f.Start:f.End] {
		case "alice@example.com":
			alice = f.Start
		case "bob@example.com":
			bob = f.Start
		default:
			t.Fatalf("offset %d:%d does not slice back to a known email, got %q", f.Start, f.End, doc[f.Start:f.End])
		}
	}
	if alice < 0 || bob < 0 {
		t.Fatalf("expected both email findings, got alice=%d bob=%d in %+v", alice, bob, findings)
	}
	return alice, bob
}

func TestIngest_JSON_DocumentRelativeOffsets(t *testing.T) {
	svc := newRoundtripService()
	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "json-offsets",
		ContentFormat: "json",
		Content:       twoLeafJSON,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Findings must be document-relative.
	alice, bob := emailOffsets(t, twoLeafJSON, ing.Findings)
	if alice <= 0 {
		t.Fatalf("first-leaf email offset must be document-relative (>0), got %d", alice)
	}
	if bob <= alice {
		t.Fatalf("second-leaf email offset must be past the first leaf: alice=%d bob=%d", alice, bob)
	}

	// TokenRef offsets must be document-relative too: slicing the submitted
	// document at each token span must reproduce the exact address.
	sawAlice, sawBob := false, false
	for _, tok := range ing.Tokens {
		if tok.EntityType != "EMAIL_ADDRESS" {
			continue
		}
		if tok.Start < 0 || tok.End > len(twoLeafJSON) || tok.Start > tok.End {
			t.Fatalf("token span out of bounds: %d:%d", tok.Start, tok.End)
		}
		switch twoLeafJSON[tok.Start:tok.End] {
		case "alice@example.com":
			sawAlice = true
		case "bob@example.com":
			if tok.Start <= alice {
				t.Fatalf("second-leaf token offset must be past the first leaf, got %d", tok.Start)
			}
			sawBob = true
		default:
			t.Fatalf("token offset %d:%d does not slice to a known email: %q", tok.Start, tok.End, twoLeafJSON[tok.Start:tok.End])
		}
	}
	if !sawAlice || !sawBob {
		t.Fatalf("expected both email tokens with document-relative offsets, alice=%v bob=%v", sawAlice, sawBob)
	}
}

func TestIngest_NDJSON_SecondLineOffsetPastFirstLine(t *testing.T) {
	svc := newRoundtripService()
	firstLineLen := len(`{"email":"alice@example.com"}`)

	ing, err := svc.Ingest(context.Background(), IngestRequest{
		TenantID:      "acme",
		ID:            "ndjson-offsets",
		ContentFormat: "ndjson",
		Content:       twoLineNDJSON,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	alice, bob := emailOffsets(t, twoLineNDJSON, ing.Findings)
	if bob <= firstLineLen {
		t.Fatalf("second-line email offset must be past the first line (len=%d), got %d", firstLineLen, bob)
	}
	if alice < 0 || alice >= firstLineLen {
		t.Fatalf("first-line email offset expected within first line, got %d", alice)
	}

	// Same guarantee on TokenRef offsets.
	sawSecondLineToken := false
	for _, tok := range ing.Tokens {
		if tok.EntityType != "EMAIL_ADDRESS" {
			continue
		}
		if twoLineNDJSON[tok.Start:tok.End] == "bob@example.com" {
			if tok.Start <= firstLineLen {
				t.Fatalf("second-line token offset must be past the first line, got %d", tok.Start)
			}
			sawSecondLineToken = true
		}
	}
	if !sawSecondLineToken {
		t.Fatalf("expected a second-line token with an offset past the first line")
	}
}

func TestSynthesize_JSON_DocumentRelativeOffsets(t *testing.T) {
	svc := newRoundtripService()
	resp, err := svc.Synthesize(context.Background(), SynthesizeRequest{
		ContentFormat: "json",
		Content:       twoLeafJSON,
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	alice, bob := emailOffsets(t, twoLeafJSON, resp.Findings)
	if alice <= 0 {
		t.Fatalf("first-leaf email offset must be document-relative (>0), got %d", alice)
	}
	if bob <= alice {
		t.Fatalf("second-leaf email offset must be past the first leaf: alice=%d bob=%d", alice, bob)
	}
}

func TestSynthesize_NDJSON_SecondLineOffsetPastFirstLine(t *testing.T) {
	svc := newRoundtripService()
	firstLineLen := len(`{"email":"alice@example.com"}`)

	resp, err := svc.Synthesize(context.Background(), SynthesizeRequest{
		ContentFormat: "ndjson",
		Content:       twoLineNDJSON,
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	alice, bob := emailOffsets(t, twoLineNDJSON, resp.Findings)
	if bob <= firstLineLen {
		t.Fatalf("second-line email offset must be past the first line (len=%d), got %d", firstLineLen, bob)
	}
	if alice < 0 || alice >= firstLineLen {
		t.Fatalf("first-line email offset expected within first line, got %d", alice)
	}
}
