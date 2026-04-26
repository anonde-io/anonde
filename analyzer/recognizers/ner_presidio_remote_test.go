package recognizers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moogacs/anonde/analyzer/recognizers"
)

func TestPresidioRemoteNERRecognizer_Analyze(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"entity_type": "PERSON",
				"start":       11,
				"end":         19,
				"score":       0.88,
			},
		})
	}))
	defer srv.Close()

	rec := recognizers.NewPresidioRemoteNERRecognizer(srv.URL)
	text := "my name is john doe"
	results, err := rec.Analyze(context.Background(), text, []string{"PERSON"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := text[results[0].Start:results[0].End]
	if got != "john doe" {
		t.Fatalf("expected span 'john doe', got %q", got)
	}
	if results[0].EntityType != "PERSON" {
		t.Fatalf("expected entity PERSON, got %s", results[0].EntityType)
	}
}
