package api

import (
	"testing"

	anondev1 "github.com/anonde-io/anonde/gen/anonde/v1"
)

func TestApplyAnalyzerOptions_RejectsExplicitZeroScoreThreshold(t *testing.T) {
	var (
		language       string
		entities       []string
		scoreThreshold = 0.7
		disableNER     bool
	)

	err := applyAnalyzerOptions(&language, &entities, &scoreThreshold, &disableNER, &anondev1.AnalyzerOptions{
		ScoreThreshold:    0,
		ScoreThresholdSet: true,
	})
	if err == nil {
		t.Fatalf("expected explicit zero score_threshold to be rejected")
	}
	if scoreThreshold != 0.7 {
		t.Fatalf("scoreThreshold changed to %v after rejected input", scoreThreshold)
	}
}

func TestApplyAnalyzerOptions_AcceptsPositiveScoreThreshold(t *testing.T) {
	var (
		language       string
		entities       []string
		scoreThreshold float64
		disableNER     bool
	)

	err := applyAnalyzerOptions(&language, &entities, &scoreThreshold, &disableNER, &anondev1.AnalyzerOptions{
		ScoreThreshold:    0.25,
		ScoreThresholdSet: true,
	})
	if err != nil {
		t.Fatalf("applyAnalyzerOptions: %v", err)
	}
	if scoreThreshold != 0.25 {
		t.Fatalf("scoreThreshold = %v, want 0.25", scoreThreshold)
	}
}
