package recognizers

import (
	"context"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
)

// spansFor runs the recognizer and returns the matched substrings.
func wellKnownOrgSpans(t *testing.T, text string) []string {
	t.Helper()
	r := NewWellKnownOrgRecognizer()
	res, err := r.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	out := make([]string, 0, len(res))
	for _, x := range res {
		if x.EntityType != "ORGANIZATION" {
			t.Fatalf("unexpected entity type %q for span %q", x.EntityType, text[x.Start:x.End])
		}
		if x.Score != wellKnownOrgScore {
			t.Fatalf("span %q scored %v, want %v", text[x.Start:x.End], x.Score, wellKnownOrgScore)
		}
		if x.RecognizerName != "WellKnownOrgRecognizer" {
			t.Fatalf("span %q has recognizer %q", text[x.Start:x.End], x.RecognizerName)
		}
		out = append(out, text[x.Start:x.End])
	}
	return out
}

func assertSpans(t *testing.T, got, want []string) {
	t.Helper()
	if len(want) == 0 {
		if len(got) > 0 {
			t.Fatalf("expected no match, got %v", got)
		}
		return
	}
	expected := map[string]int{}
	for _, w := range want {
		expected[w]++
	}
	for _, g := range got {
		if expected[g] == 0 {
			t.Fatalf("unexpected match %q (got=%v, want=%v)", g, got, want)
		}
		expected[g]--
	}
	for w, c := range expected {
		if c > 0 {
			t.Fatalf("missing match %q (got=%v, want=%v)", w, got, want)
		}
	}
}

func TestWellKnownOrgTierA(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		// Acceptance: coined AI-vendor brands as ORGANIZATION@0.85.
		{"OpenAI", "We integrated OpenAI into the pipeline.", []string{"OpenAI"}},
		{"Anthropic", "Anthropic released a new endpoint.", []string{"Anthropic"}},
		{"ChatGPT", "Users paste from ChatGPT all day.", []string{"ChatGPT"}},
		// Acceptance: multi-word brands match as ONE span.
		{"Hugging Face", "The weights are on Hugging Face now.", []string{"Hugging Face"}},
		{"Google DeepMind", "A paper from Google DeepMind dropped.", []string{"Google DeepMind"}},
		{"Stability AI", "Stability AI open-sourced the model.", []string{"Stability AI"}},
		{"Together AI", "Hosted on Together AI for cheap.", []string{"Together AI"}},
		// Phrase wins over its single-word constituent (longest-first).
		{"Google Cloud not Google", "Deployed to Google Cloud yesterday.", []string{"Google Cloud"}},
		{"Amazon Web Services not Amazon", "Billed via Amazon Web Services.", []string{"Amazon Web Services"}},
		// Big-tech single tokens.
		{"Microsoft", "Microsoft shipped the feature.", []string{"Microsoft"}},
		{"Nvidia caps variants", "Both Nvidia and NVIDIA appear.", []string{"Nvidia", "NVIDIA"}},
		{"xAI lowercase-x brand", "The xAI team posted results.", []string{"xAI"}},
		// Multiple brands in one sentence.
		{"two brands", "Compare OpenAI and Cohere on latency.", []string{"OpenAI", "Cohere"}},

		// Case-sensitivity guard: lowercase common-word homographs do NOT fire.
		{"lowercase apple fruit", "I ate an apple for lunch.", nil},
		{"lowercase amazon river", "We rafted down the amazon last year.", nil},
		{"lowercase openai", "the openai-style api was mocked in tests.", nil},
		// Word-boundary guard: no match inside an identifier.
		{"AWS inside identifier", "Set AWSInvalidTokenException on retry.", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSpans(t, wellKnownOrgSpans(t, tc.text), tc.want)
		})
	}
}

func TestWellKnownOrgTierBContextGate(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		// Fires WITH an AI/tech cue nearby.
		{"Claude with LLM cue", "We use Claude as our primary LLM assistant.", []string{"Claude"}},
		// Co-occurring Tier-A brand is (correctly) also emitted — recall-first.
		{"Claude with brand cue", "Claude, from Anthropic, drafted the summary.", []string{"Claude", "Anthropic"}},
		{"Mistral with model cue", "We fine-tuned the Mistral model overnight.", []string{"Mistral"}},
		{"Grok with brand cue", "xAI shipped Grok to Premium users.", []string{"Grok", "xAI"}},
		{"Gemini with cue", "Google's Gemini model beat the benchmark.", []string{"Gemini", "Google"}},
		{"Cursor with AI cue", "I write code in Cursor with an AI copilot.", []string{"Cursor"}},
		{"Llama with cue", "They pretrained on Llama weights first.", []string{"Llama"}},

		// Acceptance: homographs in an obviously-non-brand context do NOT fire.
		{"Claude the person", "Claude smiled and greeted his grandmother warmly.", nil},
		{"Mistral the wind", "A cold Mistral swept across the Rhone valley.", nil},
		{"Gemini the zodiac", "As a Gemini, she loved meeting new people.", nil},
		{"Grok the verb", "Grok the concept before you argue about it.", nil},
		{"Meta the prefix", "This is a Meta discussion about our process.", nil},
		{"Cursor the UI element", "Move the Cursor to the top of the screen.", nil},
		// Case-sensitivity: lowercase never fires even with a cue present.
		{"lowercase cursor + cue", "Move the cursor near the AI panel.", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSpans(t, wellKnownOrgSpans(t, tc.text), tc.want)
		})
	}
}

// TestWellKnownOrgMetaAIOneSpan confirms the Tier-A phrase "Meta AI" is emitted
// as a single span and the Tier-B "Meta" inside it is deduped away.
func TestWellKnownOrgMetaAIOneSpan(t *testing.T) {
	got := wellKnownOrgSpans(t, "Meta AI announced a new model today.")
	assertSpans(t, got, []string{"Meta AI"})
}

func TestWellKnownOrgEmptyAndShort(t *testing.T) {
	r := NewWellKnownOrgRecognizer()
	for _, txt := range []string{"", " ", "a", "Hi", ".", "\n\t", "OpenAI"} {
		res, err := r.Analyze(context.Background(), txt, nil, "en")
		if err != nil {
			t.Fatalf("Analyze(%q): %v", txt, err)
		}
		_ = res // must not panic; content asserted elsewhere
	}
}

// TestWellKnownOrgResolverConflict pins the two conflict-resolution claims from
// the research note against the real analyzer.RemoveConflicts:
//
//  1. gazetteer ORGANIZATION@0.85 beats a same-span ENAnomalyRecognizer
//     PERSON@0.25 — different entity types resolve by pure score.
//  2. a real GLiNER ORGANIZATION span wins over the gazetteer REGARDLESS of
//     score — the NER-preference rule (both ORG, NER preferred).
func TestWellKnownOrgResolverConflict(t *testing.T) {
	// Case 1: ORG@0.85 (gazetteer) vs PERSON@0.25 (anomaly) on "Claude" [0,6].
	gaz := analyzer.RecognizerResult{
		Start: 0, End: 6, Score: wellKnownOrgScore,
		EntityType: "ORGANIZATION", RecognizerName: "WellKnownOrgRecognizer",
	}
	person := analyzer.RecognizerResult{
		Start: 0, End: 6, Score: 0.25,
		EntityType: "PERSON", RecognizerName: "ENAnomalyRecognizer",
	}
	kept := analyzer.RemoveConflicts([]analyzer.RecognizerResult{gaz, person})
	if len(kept) != 1 || kept[0].EntityType != "ORGANIZATION" ||
		kept[0].RecognizerName != "WellKnownOrgRecognizer" {
		t.Fatalf("case 1: expected gazetteer ORGANIZATION to win, got %+v", kept)
	}

	// Case 2: ORG@0.85 (gazetteer) vs ORG@0.55 (GLiNER) on overlapping span.
	// The lower-scoring NER span must win via the NER-preference rule.
	ner := analyzer.RecognizerResult{
		Start: 0, End: 6, Score: 0.55,
		EntityType: "ORGANIZATION", RecognizerName: "GLiNERFlatNERRecognizer",
	}
	kept = analyzer.RemoveConflicts([]analyzer.RecognizerResult{gaz, ner})
	if len(kept) != 1 || kept[0].RecognizerName != "GLiNERFlatNERRecognizer" {
		t.Fatalf("case 2: expected GLiNER NER span to win regardless of score, got %+v", kept)
	}
}
