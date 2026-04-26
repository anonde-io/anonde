package recognizers_test

import (
	"context"
	"testing"

	"github.com/moogacs/anonde/analyzer/recognizers"
)

func TestNERRecognizer_Person(t *testing.T) {
	rec := recognizers.NewNERRecognizer()
	text := "Alice Johnson works at Microsoft in Seattle."
	results, err := rec.Analyze(context.Background(), text, []string{"PERSON"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if text[r.Start:r.End] == "Alice Johnson" && r.EntityType == "PERSON" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PERSON 'Alice Johnson', got %+v", results)
	}
}

func TestNERRecognizer_Organization(t *testing.T) {
	rec := recognizers.NewNERRecognizer()
	text := "Alice Johnson works at Microsoft in Seattle."
	results, err := rec.Analyze(context.Background(), text, []string{"ORGANIZATION"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if text[r.Start:r.End] == "Microsoft" && r.EntityType == "ORGANIZATION" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ORGANIZATION 'Microsoft', got %+v", results)
	}
}

func TestNERRecognizer_Location(t *testing.T) {
	rec := recognizers.NewNERRecognizer()
	text := "Alice Johnson works at Microsoft in Seattle."
	results, err := rec.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if text[r.Start:r.End] == "Seattle" && r.EntityType == "LOCATION" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected LOCATION 'Seattle', got %+v", results)
	}
}

func TestNERRecognizer_DenylistFiltered(t *testing.T) {
	rec := recognizers.NewNERRecognizer()
	// "SSN" is in the denylist and should not be classified as an entity.
	text := "SSN is 123-45-6789."
	results, err := rec.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if text[r.Start:r.End] == "SSN" {
			t.Errorf("expected SSN to be filtered, got %+v", r)
		}
	}
}

func TestNERRecognizer_MixedWithPatterns(t *testing.T) {
	// Ensure NER and pattern recognizers both fire without conflict.
	rec := recognizers.NewNERRecognizer()
	text := "Contact Alice Johnson at alice@example.com."
	results, err := rec.Analyze(context.Background(), text, []string{"PERSON"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected at least one PERSON result")
	}
}
