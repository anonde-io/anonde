package recognizers

import "regexp"

// English-language PERSON patterns. Complements the NER backend for
// surfaces it consistently misses on the ai4privacy_en synthetic gold:
//
//	Mr.       Mrs.      Ms.    Dr.     Prof.    Sir   Madam
//	Roma_Altenwerth     Joe_Schuster53   Edwin_Nitzsche
//
// The honorifics are a closed list; emit as PERSON so a leak-rate
// overlap counts even when the NER misses the bound name on the right.
// The underscored-username shape matches the
// First_Last(digits) pattern these synthetic corpora use; deliberately
// shape-restricted (capitalised start, underscore mid, capitalised
// continuation) to avoid FP on snake_case identifiers like
// "max_connections" or "user_id".

var (
	// Honorifics with a trailing period, optionally followed by a
	// capitalised name. The trailing-boundary is intentionally
	// permissive; the period followed by ANYTHING that isn't a word
	// character used to be a clean boundary, but ai4privacy gold has
	// run-together surfaces like "Hello Mr.Kerluke" where the period
	// is followed by an uppercase letter. We accept that case too: an
	// honorific followed by a period is a complete match regardless
	// of the next character, because no English word ends with one of
	// these specific abbreviations followed by a period.
	enHonorificRE = regexp.MustCompile(
		`\b(?:Mr|Mrs|Ms|Mx|Dr|Prof|Sir|Madam|Lord|Lady|Hon|Rev|Capt|Lt|Sgt|Col|Gen|Adm)\.`,
	)

	// First_Last underscored username; capitalised first segment,
	// underscore, capitalised second segment, optional digit suffix.
	enUnderscoredNameRE = regexp.MustCompile(
		`\b[A-Z][a-z]{2,30}_[A-Z][a-z]{2,30}\d{0,4}\b`,
	)

	// Bare-name + digit suffix ("Camilla10", "Zachariah54"). Strict
	// shape: capitalised letter, 4+ lowercase letters, then 1-4 digits.
	// The 4-letter minimum on the stem rules out "Section4", "Article2",
	// "Iso27001", and similar short-word + digit collocations that
	// commonly appear in tech and legal prose. Score is intentionally
	// lower than the underscored variant; bare "Name#" is more
	// ambiguous than "First_Last#" so a contradictory signal should
	// be able to override it.
	enNamePlusDigitsRE = regexp.MustCompile(
		`\b[A-Z][a-z]{4,30}\d{1,4}\b`,
	)
)

// NewENPersonRecognizer detects English-language PERSON surfaces the
// NER backend tends to miss: honorifics and underscored usernames.
func NewENPersonRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"ENPersonRecognizer",
		[]string{"PERSON"},
		[]string{"en"},
		[]namedPattern{
			{re: enHonorificRE, score: 0.82},
			{re: enUnderscoredNameRE, score: 0.85},
			{re: enNamePlusDigitsRE, score: 0.72},
		},
		[]string{
			"name", "patient", "customer", "doctor",
			"contact", "user", "username", "account",
		},
	)
}
