package analyzer

import (
	"testing"
)

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantClean   string
		wantMapping bool // expect non-nil offset map
	}{
		{
			name:        "no escapes",
			in:          "Plain text with no escapes.",
			wantClean:   "Plain text with no escapes.",
			wantMapping: false,
		},
		{
			name:        "color wrap",
			in:          "\x1b[31mhello\x1b[0m",
			wantClean:   "hello",
			wantMapping: true,
		},
		{
			name:        "mid-sentence escape",
			in:          "Date: \x1b[31m26.03.2003\x1b[0m end",
			wantClean:   "Date: 26.03.2003 end",
			wantMapping: true,
		},
		{
			name:        "compound escape",
			in:          "\x1b[1;33mbold yellow\x1b[0m",
			wantClean:   "bold yellow",
			wantMapping: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clean, mp := stripANSI(tc.in)
			if clean != tc.wantClean {
				t.Fatalf("clean=%q want %q", clean, tc.wantClean)
			}
			if (mp != nil) != tc.wantMapping {
				t.Fatalf("mapping presence mismatch: got nil=%v want non-nil=%v", mp == nil, tc.wantMapping)
			}
			if mp != nil && len(mp) != len(clean) {
				t.Fatalf("mapping length %d != clean length %d", len(mp), len(clean))
			}
		})
	}
}

func TestStripANSIOffsetTranslation(t *testing.T) {
	// Verify that a finding produced on the cleaned text can be
	// translated back to the correct slice of the original input.
	original := "Date: \x1b[31m26.03.2003\x1b[0m end"
	clean, mp := stripANSI(original)
	if clean != "Date: 26.03.2003 end" {
		t.Fatalf("unexpected clean: %q", clean)
	}
	// Date span in clean text is indices 6..16 (inclusive..exclusive).
	cleanStart, cleanEnd := 6, 16
	if clean[cleanStart:cleanEnd] != "26.03.2003" {
		t.Fatalf("clean date span wrong: %q", clean[cleanStart:cleanEnd])
	}
	got := translateFindings([]RecognizerResult{{Start: cleanStart, End: cleanEnd}}, mp)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	orig := original[got[0].Start:got[0].End]
	if orig != "26.03.2003" {
		t.Fatalf("translated span %q != %q", orig, "26.03.2003")
	}
}
