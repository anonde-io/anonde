package recognizers

import (
	"context"
	"testing"
)

// --- suppressAnomalyPerson unit behaviour ---------------------------------

func TestSuppressAnomalyPerson_Gates(t *testing.T) {
	cases := []struct {
		name string
		text string
		tok  string // token to locate + test
		want bool   // want suppressed
	}{
		// Structured-surface FPs → suppressed.
		{"json key colon", `{"Manager": "value"}`, "Manager", true},
		{"json key quoted", `"Password":"x"`, "Password", true},
		{"assignment equals", "Timeout=30", "Timeout", true},
		{"user-agent slash digit", "Mozilla/5.0 (Windows)", "Mozilla", true},
		{"version slash digit", "Gecko/20100101 Firefox", "Gecko", true},
		{"contraction ve", "We've shipped it", "We've", true},
		{"contraction re", "We're almost done", "We're", true},
		{"contraction im", "I'm ready to go", "I'm", true},
		{"contraction nt", "Don't worry about it", "Don't", true},

		// LEAK-SAFETY ANCHORS — real names that MUST be kept.
		{"bare name prose space", "patient Smith was admitted", "Smith", false},
		{"bare name comma", "Dear Omer, thanks", "Omer", false},
		{"bare name period", "Contact Rose.", "Rose", false},
		{"name before slash-letter", "John/Jane will attend", "John", false},
		// Possessive "'s" on a real name — the bare regex captures the clitic;
		// this leaked 17 gold spans until "'s" was dropped from the gate.
		{"possessive s name", "patient Demarco's phone", "Demarco's", false},
		{"possessive s common", "Jason's contract expires", "Jason's", false},
		// Bare colon after a name (salutation) — leaked "Jaclyn54:" until the
		// colon rule was narrowed to quoted JSON keys only.
		{"salutation bare colon", "reminder for Jaclyn54: your appt", "Jaclyn54", false},

		// Name-cue veto keeps a token even with an FP signal present.
		{"title vetoes equals", "Dr. Rose=active", "Rose", false},

		// Self-corroboration guard: a JSON key / assignment whose surface IS a
		// name-cue word ("Reported"/"Signed"/"Stated") must NOT corroborate
		// itself — the cue search excludes [start:end]. Regression for the
		// window-includes-token bug.
		{"json key is cue word", `{"Reported": 1}`, "Reported", true},
		{"assignment is cue word", "Signed=true", "Signed", true},
		{"stated json key", `{"Stated": "x"}`, "Stated", true},
		// Value-side cue must not rescue a JSON key: "said" lives in the VALUE,
		// past the '"' delimiter, so it can't corroborate the KEY "Manager".
		{"value-side verb no rescue", `{"Manager":"said no"}`, "Manager", true},
		// A REAL name whose surface is a cue word is still kept in prose (no
		// structural signal → never gated), so the guard can't leak it.
		{"cue-word name in prose", "We spoke with Said yesterday", "Said", false},

		// Multi-token never gated.
		{"multi token colon", `{"John Smith": 1}`, "John Smith", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := indexOf(tc.text, tc.tok)
			if start < 0 {
				t.Fatalf("token %q not found in %q", tc.tok, tc.text)
			}
			end := start + len(tc.tok)
			got := suppressAnomalyPerson(tc.text, start, end)
			if got != tc.want {
				t.Fatalf("suppressAnomalyPerson(%q @ %q) = %v, want %v", tc.tok, tc.text, got, tc.want)
			}
		})
	}
}

// --- end-to-end through the recognizers -----------------------------------

func enAnomalyPersons(t *testing.T, text string) map[string]bool {
	t.Helper()
	r := NewENAnomalyRecognizer()
	res, err := r.Analyze(context.Background(), text, []string{"en"}, "en")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	out := map[string]bool{}
	for _, f := range res {
		out[text[f.Start:f.End]] = true
	}
	return out
}

func TestENAnomaly_StructuralFPsDowngraded(t *testing.T) {
	// Common-word FPs on structured surfaces must NOT be emitted.
	for _, tc := range []struct{ text, tok string }{
		{`{"Manager": "Acme"}`, "Manager"},
		{`{"Password": "x"}`, "Password"},
		{"Mozilla/5.0 compatible", "Mozilla"},
		{"We've reviewed your file", "We've"},
	} {
		got := enAnomalyPersons(t, tc.text)
		if got[tc.tok] {
			t.Errorf("ENAnomaly emitted structural FP %q from %q", tc.tok, tc.text)
		}
	}
}

func TestENAnomaly_RealNamesKept(t *testing.T) {
	// A bare real name the pattern is the SOLE detector of must survive —
	// the leak-safety anchor.
	for _, tc := range []struct{ text, tok string }{
		{"The patient Smith was admitted today", "Smith"},        // bare surname, prose
		{"Dr. Rose examined the wound", "Rose"},                  // titled
		{"Please call Grace about the invoice", "Grace"},         // name-collision word kept
		{"Mark reviewed the report yesterday", "Mark"},           // name-collision word kept
		{"He met John yesterday", "John"},                        // bare first name
	} {
		got := enAnomalyPersons(t, tc.text)
		if !got[tc.tok] {
			t.Errorf("ENAnomaly dropped real name %q from %q (LEAK); got=%v", tc.tok, tc.text, got)
		}
	}
}

// --- collision-word discipline: Mark / Rose / Grace ------------------------
//
// These are both common English words AND common given names. The gate must
// key on STRUCTURE (adjacent punctuation), not on the word itself: the same
// token is dropped on a structural surface and kept in prose.

func TestCollisionWords_StructureNotLexicon(t *testing.T) {
	cases := []struct {
		text string
		tok  string
		want bool // want KEPT (emitted)
	}{
		{"Grace period expires soon", "Grace", true},      // prose → kept (can't drop, would leak "Grace" the name)
		{`{"Grace": 30}`, "Grace", false},                 // json key → dropped
		{"Mark the box below", "Mark", true},              // prose → kept
		{"Mark=true in config", "Mark", false},            // assignment → dropped
		{"Rose to the occasion", "Rose", true},            // prose → kept
	}
	for _, tc := range cases {
		got := enAnomalyPersons(t, tc.text)
		if got[tc.tok] != tc.want {
			t.Errorf("collision %q in %q: emitted=%v want=%v", tc.tok, tc.text, got[tc.tok], tc.want)
		}
	}
}
