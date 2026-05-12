package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

// ENAnomalyRecognizer detects PERSON candidates in English text using two
// complementary patterns:
//
//  1. STRUCTURAL: an English honorific or clinical label immediately followed
//     by 1–4 capitalised name tokens — "Dr. Sarah Williams", "Patient: Omar
//     Hassan". Emitted at score 0.85.
//
//  2. BARE: 1–4 contiguous capitalised name-shaped tokens with no preceding
//     title — "John Smith was admitted". Emitted at score 0.25, deliberately
//     below the typical 0.30 score threshold so it is filtered out unless a
//     context-keyword boost lifts it. The +0.35 enhancement adds when nearby
//     words match the recognizer's PERSON cues ("patient", "doctor",
//     "admitted", …), lifting clinical-context names to ~0.60 while
//     non-clinical capitalised sequences fall below threshold and are dropped.
//
// The bare path is intentionally permissive: there is no embedded English
// clinical vocabulary to discriminate "Acute Pneumonia" from "John Smith",
// only a small closed-class prefix gate that strips obvious non-PII
// sentence starters (The, He, In, …) and English honorifics that the
// structural path already captures (Mr, Dr, Patient, …). Customers tune
// remaining false positives via AnalysisConfig.AllowList.
//
// Examples (structural, score 0.85):
//   - "Mr. John Smith"            → "John Smith"
//   - "Dr. Sarah Williams"        → "Sarah Williams"
//   - "Patient: Omar Hassan"      → "Omar Hassan"
//   - "Pt. Mary Jane Watson"      → "Mary Jane Watson"   (3 tokens)
//   - "Mrs Eliza Thompson-Brown"  → "Eliza Thompson-Brown" (hyphenated)
//
// Examples (bare, score 0.25; 0.60 with context):
//   - "John Smith was admitted"   → "John Smith"
//   - "Smith called the clinic"   → "Smith"
//   - "He met John yesterday"     → "John"        ("He" gated as closed-class)
//   - "The cat ran"               → (nothing)     (all caps are closed-class)

// enAnomalyTitledRE matches an English honorific, clinical label, or
// self-introduction / sign-off anchor, then captures 1-4 capitalised name
// tokens in group 1.
//
// Name token shape: [A-Z][a-zA-Z'-]{1,30}.
//   - Apostrophe allowed: "O'Connor", "D'Angelo".
//   - Internal hyphen allowed: "Thompson-Brown", "Smith-Jones".
//   - No spaces in a single token; multi-word names span via [ \t]+.
//
// Alternation order matters: Go's RE2 picks the FIRST matching alternative,
// not the longest. Multi-word anchors must come before their single-word
// prefix ("Yours sincerely," before "Yours,"; "Best regards," before
// "Best,"). Tested by recognizers_test.go::TestENAnomalyAnchorOrder.
var enAnomalyTitledRE = regexp.MustCompile(
	`\b(?:` +
		// Honorifics — period optional ("Mr Smith" vs "Mr. Smith")
		`Mr\.?|Mrs\.?|Ms\.?|Miss|Mister|Madam|Sir|` +
		// Medical honorifics
		`Dr\.?|Prof\.?|Doctor|Professor|` +
		// Clinical labels
		`Pt\.?|Patient:?|the[ \t]+patient|` +
		// Self-introduction anchors. "I am happy" / "This is Tuesday"
		// produce single-token false positives if the name regex matches
		// the trailing capitalised word; accepted because (a) those
		// constructions are rare in business prose, and (b) the bench
		// shows net PERSON precision improves from catching the much
		// more common "I am [FirstName LastName]" pattern.
		`I[ \t]+am|I'm|My[ \t]+name[ \t]+is|This[ \t]+is|` +
		// Email sign-offs. Longer alternatives MUST come first per the
		// Go RE2 first-match rule.
		`Yours[ \t]+sincerely,?|Yours[ \t]+truly,?|Yours,?|` +
		`Best[ \t]+regards,?|Kind[ \t]+regards,?|Best,?|` +
		`Sincerely,?|Regards,?|Cheers,?` +
		`)` +
		`[ \t]+` +
		`([A-Z][a-zA-Z'-]{1,30}(?:[ \t]+[A-Z][a-zA-Z'-]{1,30}){0,3})\b`,
)

// enAnomalyBareRE matches 1–4 contiguous capitalised name-shaped tokens.
// Whitespace between tokens is [ \t]+ only — \s+ would let the pattern
// eat across a newline boundary and capture a section header as the next
// name token.
var enAnomalyBareRE = regexp.MustCompile(
	`\b[A-Z][a-zA-Z'-]{1,30}(?:[ \t]+[A-Z][a-zA-Z'-]{1,30}){0,3}\b`,
)

// enClosedClassPrefixes — English words that should never be the lead token of
// a PERSON span. When the first capitalised token of a bare match is one of
// these, the whole sequence is dropped.
//
// Coverage:
//   - Articles, demonstratives, prepositions, conjunctions, pronouns — these
//     legitimately appear capitalised at sentence starts.
//   - English honorifics (Mr, Dr, Patient, …) — the structural path already
//     captures these followed by a name; the bare path emitting "Mr" alone
//     when followed by a period (so the title-anchored regex can't span
//     them) is pure noise.
//   - Days/months — capitalised calendar tokens that are dates, not names.
//
// Deliberately NOT included: clinical section headers ("Diagnosis",
// "Treatment", "Discharge", …). Those over-fire as PERSON in clinical
// templates but adding them blurs into a domain denylist that defeats the
// "bare" pattern's value in general English text. Use AllowList for them.
var enClosedClassPrefixes = map[string]struct{}{
	// Articles & demonstratives
	"the": {}, "a": {}, "an": {},
	"this": {}, "that": {}, "these": {}, "those": {},
	// Pronouns & possessives
	"he": {}, "she": {}, "it": {}, "we": {}, "you": {}, "they": {}, "i": {},
	"my": {}, "our": {}, "your": {}, "his": {}, "her": {}, "their": {}, "its": {},
	"him": {}, "us": {}, "them": {},
	// Conjunctions & adverbs
	"and": {}, "or": {}, "but": {}, "nor": {}, "yet": {}, "so": {},
	"if": {}, "when": {}, "where": {}, "what": {}, "why": {}, "how": {},
	"then": {}, "there": {}, "here": {}, "now": {}, "today": {},
	"yesterday": {}, "tomorrow": {},
	// Prepositions
	"in": {}, "on": {}, "at": {}, "by": {}, "from": {}, "to": {},
	"for": {}, "with": {}, "of": {}, "as": {}, "into": {}, "onto": {},
	"upon": {}, "about": {}, "after": {}, "before": {}, "during": {},
	"between": {}, "among": {}, "over": {}, "under": {}, "through": {},
	// Yes/No, modals
	"yes": {}, "no": {}, "not": {}, "do": {}, "does": {}, "did": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {},
	"will": {}, "would": {}, "could": {}, "should": {}, "may": {},
	"might": {}, "must": {}, "can": {},
	// English honorifics — the structural path captures these + a name;
	// the bare path should not emit them when stripped from a name (e.g.
	// "Mr." with trailing period breaks the title-anchored regex and the
	// bare regex matches "Mr" alone — pure noise).
	"mr": {}, "mrs": {}, "ms": {}, "miss": {}, "mister": {}, "madam": {},
	"sir": {}, "dr": {}, "prof": {}, "doctor": {}, "professor": {},
	"pt": {}, "patient": {}, "nurse": {}, "physician": {},
	// Calendar
	"monday": {}, "tuesday": {}, "wednesday": {}, "thursday": {},
	"friday": {}, "saturday": {}, "sunday": {},
	"january": {}, "february": {}, "march": {}, "april": {},
	"june": {}, "july": {}, "august": {}, "september": {}, "october": {},
	"november": {}, "december": {},
}

// enPersonContextKeywords — words that, when appearing within the analyzer's
// context window of a bare-name finding, boost its score (default +0.35).
// Curated for clinical / records context: surrounding language about
// patients, providers, encounters, and admin metadata.
//
// Non-clinical PERSON cues (self-introductions, email signatures, customer
// support text) are NOT keywords — they are handled by structural anchors
// in enAnomalyTitledRE ("I am", "Sincerely,", "Regards,", …) so that the
// anchor + 1–4 caps token shape is required, instead of any nearby
// business word boosting any capitalised pair (which produced many FPs:
// "Email", "Northwind Health", "INV-" being treated as PERSON when a
// keyword like "email" or "phone" appeared in the same sentence).
var enPersonContextKeywords = []string{
	// Clinical roles
	"patient", "patients", "doctor", "doctors", "physician", "physicians",
	"nurse", "nurses", "surgeon", "surgeons", "clinician", "provider",
	"caregiver", "therapist", "psychiatrist", "consultant",
	// Clinical encounter verbs
	"admitted", "discharged", "diagnosed", "treated", "examined", "assessed",
	"prescribed", "referred", "seen", "reviewed", "consulted", "evaluated",
	"presented", "complained",
	// Identification / metadata
	"name", "named", "born", "dob", "mrn", "id", "patientid",
	// Common honorific cues (lowercase forms — uppercase forms are handled
	// by the structural recognizer path)
	"mr", "mrs", "ms", "dr", "prof",
	// Generic person-ish narrative
	"called", "spoke", "wrote", "phoned", "contacted", "visited",
}

// isLeadTokenGated reports whether tok must not be the leader of a bare PERSON
// span. Two gates:
//
//  1. Closed-class lookup (lowercased): articles, pronouns, prepositions,
//     conjunctions, honorifics, days, months.
//  2. Short all-caps acronyms (2–5 letters, every char A-Z): MRN, DOB, ID,
//     USA, NHS, ICU, … Almost never proper names in clinical or business
//     prose and a very common FP shape. Trades a small recall loss on
//     hypothetical "JOHN SMITH"-style all-caps names for a meaningful
//     precision win on acronyms.
func isLeadTokenGated(tok string) bool {
	if _, ok := enClosedClassPrefixes[strings.ToLower(tok)]; ok {
		return true
	}
	if n := len(tok); n >= 2 && n <= 5 {
		allUpper := true
		for i := 0; i < n; i++ {
			c := tok[i]
			if c < 'A' || c > 'Z' {
				allUpper = false
				break
			}
		}
		if allUpper {
			return true
		}
	}
	return false
}

// ENAnomalyRecognizer recognises English-language PERSON candidates by
// title-anchored capture and bare capitalised-token capture.
type ENAnomalyRecognizer struct{}

// NewENAnomalyRecognizer constructs the recognizer.
func NewENAnomalyRecognizer() *ENAnomalyRecognizer { return &ENAnomalyRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *ENAnomalyRecognizer) Name() string { return "ENAnomalyRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *ENAnomalyRecognizer) SupportedEntities() []string { return []string{"PERSON"} }

// SupportedLanguages returns the languages this recognizer applies to.
func (r *ENAnomalyRecognizer) SupportedLanguages() []string { return []string{"en"} }

// ContextKeywords returns the PERSON cue words used by the analyzer's
// context-keyword score enhancer. A bare-name finding within
// ContextEnhancement.WindowChars of any of these gets a score boost
// (default +0.35), lifting clinical-context names cleanly above the
// default 0.3 score threshold while leaving non-clinical capitalised
// sequences near the floor.
func (r *ENAnomalyRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{
		"PERSON": enPersonContextKeywords,
	}
}

// Analyze emits PERSON findings for title-anchored matches (score 0.85)
// and bare capitalised-token matches (score 0.25). Same-span findings
// from both paths are deduplicated, keeping the higher score.
func (r *ENAnomalyRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	// Dedupe by (start, end): the bare regex frequently matches the same
	// span as the title-anchored capture (e.g. "John Smith" appears both
	// inside "Mr. John Smith" via the structural path and as a standalone
	// capitalised pair via the bare path). Keep the higher score.
	bestByKey := map[[2]int]float64{}

	emit := func(start, end int, score float64) {
		key := [2]int{start, end}
		if cur, ok := bestByKey[key]; ok && cur >= score {
			return
		}
		bestByKey[key] = score
	}

	for _, m := range enAnomalyTitledRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		emit(m[2], m[3], 0.85)
	}

	for _, m := range enAnomalyBareRE.FindAllStringIndex(text, -1) {
		matchEnd := m[1]
		// Strip leading closed-class tokens from the match. The regex is
		// greedy, so "The Smith family" matches "The Smith" as a 2-token
		// sequence; dropping the whole match loses recall on the trailing
		// real name. We walk tokens left-to-right, advancing past
		// closed-class leaders until we find the first content token,
		// then emit from there to the end of the match.
		cursor := m[0]
		for cursor < matchEnd {
			tokStart := cursor
			for cursor < matchEnd && text[cursor] != ' ' && text[cursor] != '\t' {
				cursor++
			}
			tok := text[tokStart:cursor]
			if !isLeadTokenGated(tok) {
				emit(tokStart, matchEnd, 0.25)
				break
			}
			for cursor < matchEnd && (text[cursor] == ' ' || text[cursor] == '\t') {
				cursor++
			}
		}
	}

	if len(bestByKey) == 0 {
		return nil, nil
	}
	out := make([]analyzer.RecognizerResult, 0, len(bestByKey))
	for key, score := range bestByKey {
		out = append(out, analyzer.RecognizerResult{
			Start:          key[0],
			End:            key[1],
			Score:          score,
			EntityType:     "PERSON",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}

