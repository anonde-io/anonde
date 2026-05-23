package recognizers

import "regexp"

// English-language PERSON patterns. Complements the NER backend for
// surfaces it consistently misses on the ai4privacy_en synthetic gold:
//
//	Mr.       Mrs.      Ms.    Dr.     Prof.    Sir   Madam
//	Roma_Altenwerth     Joe_Schuster53   Edwin_Nitzsche
//
// The honorifics are a closed list — emit as PERSON so a leak-rate
// overlap counts even when the NER misses the bound name on the right.
// The underscored-username shape matches the
// First_Last(digits) pattern these synthetic corpora use; deliberately
// shape-restricted (capitalised start, underscore mid, capitalised
// continuation) to avoid FP on snake_case identifiers like
// "max_connections" or "user_id".

var (
	// Honorifics with a trailing period. The pattern requires a word
	// boundary at the start AND a period (or whitespace+capital after)
	// at the end so we don't match "drive" as "dr." inside a word.
	enHonorificRE = regexp.MustCompile(
		`\b(?:Mr|Mrs|Ms|Mx|Dr|Prof|Sir|Madam|Lord|Lady|Hon|Rev|Capt|Lt|Sgt|Col|Gen|Adm)` +
			`\.(?:\s+[A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)?\b`,
	)

	// First_Last underscored username — capitalised first segment,
	// underscore, capitalised second segment, optional digit suffix.
	enUnderscoredNameRE = regexp.MustCompile(
		`\b[A-Z][a-z]{2,30}_[A-Z][a-z]{2,30}\d{0,4}\b`,
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
		},
		[]string{
			"name", "patient", "customer", "doctor",
			"contact", "user", "username", "account",
		},
	)
}
