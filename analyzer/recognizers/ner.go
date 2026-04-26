package recognizers

import (
	"context"
	"strings"

	"github.com/jdkato/prose/v2"
	"github.com/moogacs/anonde/analyzer"
)

// proseLabel maps prose BIO entity types to Presidio entity types.
var proseLabel = map[string]string{
	"PERSON":       "PERSON",
	"GPE":          "LOCATION", // geopolitical: countries, cities, states
	"LOC":          "LOCATION",
	"FAC":          "LOCATION", // facilities (airports, buildings)
	"ORG":          "ORGANIZATION",
	"ORGANIZATION": "ORGANIZATION",
	"NORP":         "NRP", // nationalities, religions, political groups
}

// nerDenylist suppresses common prose false positives caused by sentence-initial
// capitalisation, POS tagger noise, and known abbreviations.
var nerDenylist = map[string]struct{}{
	// Greetings / filler
	"hi": {}, "hello": {}, "hey": {}, "dear": {}, "sir": {}, "ok": {},
	"yes": {}, "no": {}, "please": {}, "thanks": {}, "thank": {},
	// Pronouns
	"i": {}, "my": {}, "me": {}, "we": {}, "our": {}, "us": {},
	// Common abbreviations that prose misclassifies as entities
	"ssn": {}, "dob": {}, "dba": {}, "url": {}, "api": {}, "ceo": {},
	"cto": {}, "cfo": {}, "inc": {}, "llc": {}, "ltd": {}, "etc": {},
	"mr": {}, "mrs": {}, "ms": {}, "dr": {}, "prof": {},
	// Common English nouns misclassified at sentence start
	"society": {}, "company": {}, "group": {}, "team": {}, "board": {},
	"office": {}, "center": {}, "institute": {}, "association": {},
	"department": {}, "division": {},
}

const nerMinLen = 3

// NERRecognizer detects PERSON, LOCATION, ORGANIZATION, and NRP entities
// using prose's chunker-based Named Entity Recognition model.
type NERRecognizer struct{}

func NewNERRecognizer() *NERRecognizer { return &NERRecognizer{} }

func (r *NERRecognizer) Name() string { return "NERRecognizer" }
func (r *NERRecognizer) SupportedEntities() []string {
	return []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"}
}
func (r *NERRecognizer) SupportedLanguages() []string { return []string{"en"} }

func (r *NERRecognizer) Analyze(_ context.Context, text string, entities []string, _ string) ([]analyzer.RecognizerResult, error) {
	doc, err := prose.NewDocument(text)
	if err != nil {
		return nil, err
	}

	wantAll := len(entities) == 0
	want := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		want[e] = struct{}{}
	}

	// Walk tokens in source order, reconstructing byte offsets by forward scan.
	// prose emits B-TYPE for every token (no I- continuation), so we post-merge
	// adjacent results of the same type separated only by whitespace.
	cursor := 0

	type span struct {
		start   int
		end     int
		bioType string
	}
	var current *span
	var raw []analyzer.RecognizerResult

	accept := func(s *span) {
		entityText := text[s.start:s.end]
		if len(entityText) < nerMinLen {
			return
		}
		if _, denied := nerDenylist[strings.ToLower(entityText)]; denied {
			return
		}
		presidioType, ok := proseLabel[s.bioType]
		if !ok {
			return
		}
		if !wantAll {
			if _, ok := want[presidioType]; !ok {
				return
			}
		}
		raw = append(raw, analyzer.RecognizerResult{
			Start:          s.start,
			End:            s.end,
			Score:          0.75,
			EntityType:     presidioType,
			RecognizerName: "NERRecognizer",
		})
	}

	flush := func() {
		if current != nil {
			accept(current)
			current = nil
		}
	}

	for _, tok := range doc.Tokens() {
		label := tok.Label // "B-PERSON", "I-GPE", "O", ""

		idx := strings.Index(text[cursor:], tok.Text)
		if idx < 0 {
			flush()
			continue
		}
		tokStart := cursor + idx
		tokEnd := tokStart + len(tok.Text)
		cursor = tokEnd

		if strings.HasPrefix(label, "B-") {
			flush()
			current = &span{start: tokStart, end: tokEnd, bioType: label[2:]}
		} else if strings.HasPrefix(label, "I-") && current != nil && label[2:] == current.bioType {
			current.end = tokEnd
		} else {
			flush()
		}
	}
	flush()

	// Merge adjacent results of the same Presidio entity type when separated
	// only by whitespace — handles prose's per-token B- tagging of multi-word names.
	return mergeAdjacent(raw, text), nil
}

// mergeAdjacent merges consecutive results of the same entity type when the
// text between them contains only whitespace (e.g. "Alice" + "Johnson" → "Alice Johnson").
func mergeAdjacent(results []analyzer.RecognizerResult, text string) []analyzer.RecognizerResult {
	if len(results) == 0 {
		return results
	}
	merged := results[:1:1]
	merged = append(merged[:0:0], results[0])
	for _, r := range results[1:] {
		last := &merged[len(merged)-1]
		if r.EntityType == last.EntityType && last.RecognizerName == "NERRecognizer" {
			between := text[last.End:r.Start]
			if strings.TrimSpace(between) == "" {
				last.End = r.End
				continue
			}
		}
		merged = append(merged, r)
	}
	return merged
}
