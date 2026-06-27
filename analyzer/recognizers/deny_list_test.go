package recognizers

import (
	"context"
	"testing"
)

func TestDenyListRecognizerFindsTerms(t *testing.T) {
	rec := NewDenyListRecognizer([]string{"Acme", "ProjectFalcon"}, "CUSTOM")
	text := "Acme launched ProjectFalcon with acme staff"

	got, err := rec.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Acme (case-insensitive) appears twice, ProjectFalcon once.
	if len(got) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.EntityType != "CUSTOM" {
			t.Fatalf("expected CUSTOM entity, got %q", r.EntityType)
		}
		if r.RecognizerName != "DenyListRecognizer" {
			t.Fatalf("expected DenyListRecognizer name, got %q", r.RecognizerName)
		}
		surface := text[r.Start:r.End]
		if surface != "Acme" && surface != "acme" && surface != "ProjectFalcon" {
			t.Fatalf("unexpected matched surface %q", surface)
		}
	}
}

func TestDenyListRecognizerDefaultsToCUSTOM(t *testing.T) {
	rec := NewDenyListRecognizer([]string{"widget"}, "")
	if ents := rec.SupportedEntities(); len(ents) != 1 || ents[0] != "CUSTOM" {
		t.Fatalf("expected default CUSTOM entity, got %v", ents)
	}
	got, _ := rec.Analyze(context.Background(), "a widget here", nil, "en")
	if len(got) != 1 || got[0].EntityType != "CUSTOM" {
		t.Fatalf("expected one CUSTOM finding, got %+v", got)
	}
}

func TestDenyListRecognizerWholeWord(t *testing.T) {
	rec := NewDenyListRecognizer([]string{"acme"}, "CUSTOM")
	// Word-character-edged term must not fire inside a larger word.
	got, _ := rec.Analyze(context.Background(), "acmecorp is not acme", nil, "en")
	if len(got) != 1 {
		t.Fatalf("expected 1 whole-word match (not inside 'acmecorp'), got %d: %+v", len(got), got)
	}
	if got[0].Start != len("acmecorp is not ") {
		t.Fatalf("expected match at the standalone 'acme', got start=%d", got[0].Start)
	}
}

func TestDenyListRecognizerNonWordEdges(t *testing.T) {
	// A term whose edges are non-word chars falls back to literal matching
	// (no \b boundary that would never hold).
	rec := NewDenyListRecognizer([]string{"@acme", "C++"}, "CUSTOM")
	got, _ := rec.Analyze(context.Background(), "ping @acme about C++ code", nil, "en")
	if len(got) != 2 {
		t.Fatalf("expected 2 findings for non-word-edged terms, got %d: %+v", len(got), got)
	}
}

func TestDenyListRecognizerIgnoresBlankAndDupes(t *testing.T) {
	rec := NewDenyListRecognizer([]string{"acme", "  ", "", "ACME"}, "CUSTOM")
	// Blank terms dropped; "acme"/"ACME" dedupe case-insensitively to one pattern.
	got, _ := rec.Analyze(context.Background(), "acme acme", nil, "en")
	if len(got) != 2 {
		t.Fatalf("expected 2 occurrences from one deduped pattern, got %d: %+v", len(got), got)
	}
}

func TestDenyListRecognizerEmpty(t *testing.T) {
	rec := NewDenyListRecognizer(nil, "CUSTOM")
	got, err := rec.Analyze(context.Background(), "nothing to find", nil, "en")
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no findings, got %+v err %v", got, err)
	}
	if langs := rec.SupportedLanguages(); len(langs) != 1 || langs[0] != "*" {
		t.Fatalf("expected wildcard language support, got %v", langs)
	}
}
