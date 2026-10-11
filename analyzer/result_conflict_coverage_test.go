package analyzer

import "testing"

// A narrower NER span must never evict a wider same-type pattern span:
// GLiNER "Jane" (1.0) displacing heuristic "Jane Doe" (0.6) leaked the surname.
func TestRemoveConflicts_NERMustNotShrinkSameTypeCoverage(t *testing.T) {
	const text = "Jane Doe (DOB 1987-03-14) reported a dispute."

	ner := RecognizerResult{Start: 0, End: 4, Score: 1.0, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"}
	pattern := RecognizerResult{Start: 0, End: 8, Score: 0.6, EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer"}

	// Both orderings must resolve alike: SortResults puts the higher-scoring
	// NER span first, so the guard has to hold in both branches.
	for _, tc := range []struct {
		name string
		in   []RecognizerResult
	}{
		{"ner first", []RecognizerResult{ner, pattern}},
		{"pattern first", []RecognizerResult{pattern, ner}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RemoveConflicts(append([]RecognizerResult(nil), tc.in...))
			if len(got) != 1 {
				t.Fatalf("got %d findings, want 1: %+v", len(got), got)
			}
			if got[0].Start != 0 || got[0].End != 8 {
				t.Errorf("kept [%d:%d] (%q), want [0:8] (%q) — surname would leak",
					got[0].Start, got[0].End, text[got[0].Start:got[0].End], text[0:8])
			}
		})
	}
}

// NER preference still applies when coverage is equal or NER is wider.
func TestRemoveConflicts_NERPreferenceStillApplies(t *testing.T) {
	tests := []struct {
		name            string
		ner, pattern    RecognizerResult
		wantRecognizer  string
		wantStart, want int
	}{
		{
			name:           "identical spans keep NER",
			ner:            RecognizerResult{Start: 0, End: 8, Score: 0.45, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},
			pattern:        RecognizerResult{Start: 0, End: 8, Score: 0.85, EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer"},
			wantRecognizer: "GLiNERRecognizer",
			wantStart:      0, want: 8,
		},
		{
			name:           "wider NER span keeps NER",
			ner:            RecognizerResult{Start: 0, End: 12, Score: 0.45, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},
			pattern:        RecognizerResult{Start: 0, End: 8, Score: 0.85, EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer"},
			wantRecognizer: "GLiNERRecognizer",
			wantStart:      0, want: 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveConflicts([]RecognizerResult{tt.ner, tt.pattern})
			if len(got) != 1 {
				t.Fatalf("got %d findings, want 1: %+v", len(got), got)
			}
			if got[0].RecognizerName != tt.wantRecognizer {
				t.Errorf("kept %s, want %s", got[0].RecognizerName, tt.wantRecognizer)
			}
			if got[0].Start != tt.wantStart || got[0].End != tt.want {
				t.Errorf("kept [%d:%d], want [%d:%d]", got[0].Start, got[0].End, tt.wantStart, tt.want)
			}
		})
	}
}

// Kept spans must still cover every character the inputs covered.
func TestRemoveConflicts_ContainmentNeverLosesCoveredChars(t *testing.T) {
	cases := [][]RecognizerResult{
		{
			{Start: 0, End: 4, Score: 1.0, EntityType: "PERSON", RecognizerName: "GLiNERRecognizer"},
			{Start: 0, End: 8, Score: 0.6, EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer"},
		},
		{
			{Start: 2, End: 6, Score: 0.9, EntityType: "PERSON", RecognizerName: "GLiNERPool"},
			{Start: 0, End: 10, Score: 0.3, EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer"},
		},
	}
	for i, in := range cases {
		covered := map[int]bool{}
		for _, r := range in {
			for c := r.Start; c < r.End; c++ {
				covered[c] = true
			}
		}
		kept := RemoveConflicts(append([]RecognizerResult(nil), in...))
		keptCover := map[int]bool{}
		for _, r := range kept {
			for c := r.Start; c < r.End; c++ {
				keptCover[c] = true
			}
		}
		for c := range covered {
			if !keptCover[c] {
				t.Errorf("case %d: char offset %d was covered by an input span but is unredacted after conflict resolution", i, c)
			}
		}
	}
}
