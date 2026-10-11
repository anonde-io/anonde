package analyzer

import (
	"strings"
	"testing"
)

func personKeywords() map[string][]string {
	return map[string][]string{"PERSON": {"dob", "patient", "called", "mr"}}
}

// A cue in a neighbouring sentence must not promote a weak heuristic PERSON
// candidate: "DOB" one sentence earlier redacted the verb "Reach" as a name.
func TestEnhanceWithContext_HeuristicPersonNotCorroboratedAcrossSentences(t *testing.T) {
	text := "Jane Doe (DOB 1987-03-14) from Singapore. Reach her at jane.doe@northwind.com."
	at := strings.Index(text, "Reach")
	if at < 0 {
		t.Fatalf("fixture lost %q", "Reach")
	}
	reach := RecognizerResult{
		Start: at, End: at + len("Reach"), Score: 0.25,
		EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer",
	}

	out := EnhanceWithContext(text, []RecognizerResult{reach}, personKeywords(), DefaultContextEnhancement())
	if out[0].Score != 0.25 {
		t.Errorf("score = %v, want 0.25 unboosted: %q is corroborated only by \"DOB\" in the previous sentence",
			out[0].Score, text[reach.Start:reach.End])
	}
}

// The clamp must still boost a genuine in-sentence cue, or it trades a false
// positive for a leak.
func TestEnhanceWithContext_HeuristicPersonStillBoostedWithinSentence(t *testing.T) {
	text := "Jane Doe (DOB 1987-03-14) from Singapore. Reach her at jane.doe@northwind.com."
	at := strings.Index(text, "Jane Doe")
	if at < 0 {
		t.Fatalf("fixture lost %q", "Jane Doe")
	}
	jane := RecognizerResult{
		Start: at, End: at + len("Jane Doe"), Score: 0.25,
		EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer",
	}

	out := EnhanceWithContext(text, []RecognizerResult{jane}, personKeywords(), DefaultContextEnhancement())
	if out[0].Score <= 0.25 {
		t.Errorf("score = %v, want boosted: \"DOB\" is in the same sentence as %q",
			out[0].Score, text[jane.Start:jane.End])
	}
}

// Recognizers outside the set keep the full window, so structured types that
// legitimately rely on a neighbouring sentence are unaffected.
func TestEnhanceWithContext_StructuredTypesKeepCrossSentenceWindow(t *testing.T) {
	text := "Please update my credit card. The number is 4111111111111111."
	const number = "4111111111111111"
	at := strings.Index(text, number)
	if at < 0 {
		t.Fatalf("fixture lost %q", number)
	}
	cc := RecognizerResult{
		Start: at, End: at + len(number), Score: 0.3,
		EntityType: "CREDIT_CARD", RecognizerName: "CreditCardRecognizer",
	}

	kws := map[string][]string{"CREDIT_CARD": {"credit card", "card"}}
	out := EnhanceWithContext(text, []RecognizerResult{cc}, kws, DefaultContextEnhancement())
	if out[0].Score <= 0.3 {
		t.Errorf("score = %v, want boosted: a structured finding must still see the previous sentence", out[0].Score)
	}
}

func TestSentenceSplitting_DoesNotSplitOnIntraWordPeriods(t *testing.T) {
	tests := []struct {
		name, before, wantTail string
	}{
		{"email keeps window", "contact jane.doe@northwind.com about the dob of ", "contact jane.doe@northwind.com about the dob of "},
		{"decimal keeps window", "the bill was 1.5 million for patient ", "the bill was 1.5 million for patient "},
		{"hostname keeps window", "see docs.anonde.io for the patient ", "see docs.anonde.io for the patient "},
		{"real sentence end splits", "patient was admitted. then ", "then "},
		{"newline splits", "patient was admitted\nthen ", "then "},
		{"no terminator keeps all", "the patient ", "the patient "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sentenceTailOf(tt.before); got != tt.wantTail {
				t.Errorf("sentenceTailOf(%q) = %q, want %q", tt.before, got, tt.wantTail)
			}
		})
	}
}

func TestSentenceSplitting_HeadStopsAtTerminator(t *testing.T) {
	tests := []struct {
		name, after, wantHead string
	}{
		{"stops at period", " was admitted. the dob is", " was admitted"},
		{"stops at newline", " was admitted\nthe dob is", " was admitted"},
		{"keeps decimals", " owes 1.5 million and is a patient", " owes 1.5 million and is a patient"},
		{"trailing period", " was admitted.", " was admitted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sentenceHeadOf(tt.after); got != tt.wantHead {
				t.Errorf("sentenceHeadOf(%q) = %q, want %q", tt.after, got, tt.wantHead)
			}
		})
	}
}
