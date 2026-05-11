package recognizers_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/moogacs/anonde/analyzer/recognizers"
)

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func TestHugotNERRecognizer_Name(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{})
	if got := rec.Name(); got != "HugotNERRecognizer" {
		t.Fatalf("expected HugotNERRecognizer, got %q", got)
	}
}

func TestHugotNERRecognizer_SupportedEntities(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{})
	want := map[string]bool{
		"PERSON":       true,
		"LOCATION":     true,
		"ORGANIZATION": true,
		"NRP":          true,
	}
	got := rec.SupportedEntities()
	if len(got) != len(want) {
		t.Fatalf("expected %d entities, got %d: %v", len(want), len(got), got)
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected entity type %q", e)
		}
	}
}

func TestHugotNERRecognizer_SupportedLanguages(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{})
	langs := rec.SupportedLanguages()
	got := make(map[string]bool, len(langs))
	for _, l := range langs {
		got[l] = true
	}
	// Must include the languages the default multilingual model is trained
	// on. Adding more is fine; removing en/de breaks the German-by-default
	// product promise.
	for _, must := range []string{"en", "de", "es", "fr", "it"} {
		if !got[must] {
			t.Errorf("SupportedLanguages missing required language %q (got %v)", must, langs)
		}
	}
}

// ---------------------------------------------------------------------------
// Config defaults
// ---------------------------------------------------------------------------

func TestHugotNERRecognizer_DefaultModelName(t *testing.T) {
	t.Parallel()
	// Indirectly verify the default model name is set by checking the error
	// message when the model directory is missing and AutoDownload is false.
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    t.TempDir(), // exists but model subdir absent
		AutoDownload: false,
	})
	_, err := rec.Analyze(context.Background(), "hello world", nil, "en")
	if err == nil {
		t.Fatal("expected error when model is absent and AutoDownload is false")
	}
	// Default model name should appear in the error message.
	if !strings.Contains(err.Error(), "Xenova/distilbert-base-multilingual-cased-ner-hrl") &&
		!strings.Contains(err.Error(), "Xenova_distilbert-base-multilingual-cased-ner-hrl") {
		t.Errorf("expected default model name in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestHugotNERRecognizer_MissingModelNoDownload(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    t.TempDir(),
		ModelName:    "dslim/bert-base-NER",
		AutoDownload: false,
	})
	_, err := rec.Analyze(context.Background(), "Alice works at Acme.", nil, "en")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestHugotNERRecognizer_InitErrorIsCached(t *testing.T) {
	t.Parallel()
	// Calling Analyze multiple times after a failed init should return the same
	// error every time (sync.Once semantics) without panicking.
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    t.TempDir(),
		ModelName:    "dslim/bert-base-NER",
		AutoDownload: false,
	})
	_, err1 := rec.Analyze(context.Background(), "text", nil, "en")
	_, err2 := rec.Analyze(context.Background(), "text", nil, "en")
	if err1 == nil || err2 == nil {
		t.Fatal("expected errors on both calls")
	}
	if err1.Error() != err2.Error() {
		t.Errorf("expected identical cached error; got\n  %v\n  %v", err1, err2)
	}
}

func TestHugotNERRecognizer_DestroyBeforeInit(t *testing.T) {
	t.Parallel()
	// Destroy before any Analyze call must not panic.
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{})
	if err := rec.Destroy(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHugotNERRecognizer_ContextCancelledDuringAnalyze(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    t.TempDir(),
		ModelName:    "dslim/bert-base-NER",
		AutoDownload: false,
	})
	_, err := rec.Analyze(ctx, "text", nil, "en")
	// Expect some error; may be the model-not-found error or a context error.
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Label mapping (MapHugotLabel)
// ---------------------------------------------------------------------------

func TestMapHugotLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		want    string
		wantOK  bool
	}{
		// Aggregated group labels (simple aggregation mode).
		{"PER", "PERSON", true},
		{"LOC", "LOCATION", true},
		{"ORG", "ORGANIZATION", true},
		{"MISC", "NRP", true},
		// Case-insensitive.
		{"per", "PERSON", true},
		{"loc", "LOCATION", true},
		{"org", "ORGANIZATION", true},
		{"misc", "NRP", true},
		// BIO-prefixed (non-aggregated output).
		{"B-PER", "PERSON", true},
		{"I-PER", "PERSON", true},
		{"B-LOC", "LOCATION", true},
		{"I-LOC", "LOCATION", true},
		{"B-ORG", "ORGANIZATION", true},
		{"I-ORG", "ORGANIZATION", true},
		{"B-MISC", "NRP", true},
		{"I-MISC", "NRP", true},
		// "O" outside tag — should be unknown.
		{"O", "", false},
		// Totally unknown label.
		{"UNKNOWN", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, ok := recognizers.MapHugotLabel(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("MapHugotLabel(%q) ok=%v, want %v", tc.input, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("MapHugotLabel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration test (skipped in short mode — requires model download)
// ---------------------------------------------------------------------------

func TestHugotNERRecognizer_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test: requires model download (~400 MB)")
	}

	modelsDir := t.TempDir()
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    modelsDir,
		ModelName:    "Xenova/distilbert-base-multilingual-cased-ner-hrl",
		AutoDownload: true,
	})
	t.Cleanup(func() { _ = rec.Destroy() })

	text := "Alice Johnson works at Microsoft in Seattle."
	results, err := rec.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one entity, got none")
	}

	found := make(map[string]string) // entity type → matched text
	for _, r := range results {
		found[r.EntityType] = text[r.Start:r.End]
	}

	wantEntities := []struct {
		typ  string
		text string
	}{
		{"PERSON", "Alice Johnson"},
		{"ORGANIZATION", "Microsoft"},
		{"LOCATION", "Seattle"},
	}
	for _, we := range wantEntities {
		got, ok := found[we.typ]
		if !ok {
			t.Errorf("expected entity %s=%q, not found in results %v", we.typ, we.text, found)
			continue
		}
		if got != we.text {
			t.Errorf("entity %s: expected %q, got %q", we.typ, we.text, got)
		}
	}

	// Scores should be in [0, 1].
	for _, r := range results {
		if r.Score < 0 || r.Score > 1 {
			t.Errorf("score out of range for %s: %f", r.EntityType, r.Score)
		}
	}

	// RecognizerName must be set.
	for _, r := range results {
		if r.RecognizerName != "HugotNERRecognizer" {
			t.Errorf("unexpected recognizer name: %q", r.RecognizerName)
		}
	}

	// Spans must be valid substrings.
	for _, r := range results {
		if r.Start < 0 || r.End > len(text) || r.Start >= r.End {
			t.Errorf("invalid span [%d, %d] for text len %d", r.Start, r.End, len(text))
		}
		_ = errors.New // satisfy import
	}
}

// TestHugotNERRecognizer_IntegrationGerman verifies the default multilingual
// model picks up German PERSON / LOCATION / ORGANIZATION entities — the
// "German by default" product promise.
func TestHugotNERRecognizer_IntegrationGerman(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test: requires model download (~400 MB)")
	}

	modelsDir := t.TempDir()
	rec := recognizers.NewHugotNERRecognizer(recognizers.HugotNERConfig{
		ModelsDir:    modelsDir,
		AutoDownload: true, // ModelName left empty -> use default
	})
	t.Cleanup(func() { _ = rec.Destroy() })

	text := "Frau Müller wurde am 12.05.2023 in der Charité in Berlin behandelt."
	results, err := rec.Analyze(context.Background(), text, nil, "de")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one entity in German text, got none")
	}

	gotTypes := make(map[string]bool)
	for _, r := range results {
		gotTypes[r.EntityType] = true
	}
	for _, want := range []string{"PERSON", "ORGANIZATION", "LOCATION"} {
		if !gotTypes[want] {
			t.Errorf("expected %s in German NER output, got types %v (results=%+v)", want, gotTypes, results)
		}
	}
}
