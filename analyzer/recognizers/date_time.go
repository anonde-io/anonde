package recognizers

import "regexp"

// Multilingual month-name alternation covering EN / DE / ES / FR / IT,
// both full and abbreviated forms. Built as a separate constant because
// it's reused in three of the date patterns below (Month DD YYYY, DD
// Month YYYY, Month/YY slashed-shorthand). `(?i)` is set at the outer
// regex level so this alternation is case-insensitive; the non-ASCII
// chars (ä, é, à, ç) are matched literally — Go's RE2 treats them as
// regular runes inside `(?i)` and pairs them with their canonical
// case variants per Unicode case folding.
const monthsMulti = `Jan(?:uar|uary)?|Feb(?:ruar|ruary)?|M(?:är(?:z)?|ar(?:ch|zo|s)?|aggio|ag)|Apr(?:il|ile)?|May|Mai|Mayo|Jun(?:e|i|io|in|o)?|Jul(?:y|i|io|let|io)?|Aug(?:ust|usto)?|Sep(?:t(?:ember|iembre|embre)?)?|Okt(?:ober)?|Oct(?:ober|ubre|obre)?|Nov(?:ember|iembre|embre)?|Dec(?:ember|embre)?|Dez(?:ember)?|Dic(?:iembre)?|janvier|f(?:é|e)vrier|mars|avril|juin|juillet|ao(?:û|u)t|septembre|octobre|novembre|d(?:é|e)cembre|enero|febrero|marzo|abril|mayo|junio|julio|agosto|septiembre|octubre|noviembre|diciembre|gennaio|febbraio|marzo|aprile|maggio|giugno|luglio|agosto|settembre|ottobre|novembre|dicembre`

var dateTimeRE = regexp.MustCompile(
	// ISO 8601
	`\b\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])(?:[T ]\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:?\d{2})?)?\b|` +
		// MM/DD/YYYY (US) — requires month 1-12 in first slot
		`\b(?:0?[1-9]|1[0-2])[/\-.](?:0?[1-9]|[12]\d|3[01])[/\-.]\d{2,4}\b|` +
		// DD/MM/YYYY (EU) — first slot is day (1-31), second is month (1-12).
		// Needed because the US pattern above rejects e.g. "28/08/1970"
		// (slot 1 = 28, > 12). Constrained to 4-digit year to avoid
		// FP-ing on bare numeric triples like "12/15/30".
		`\b(?:0?[1-9]|[12]\d|3[01])[/\-.](?:0?[1-9]|1[0-2])[/\-.]\d{4}\b|` +
		// English Month name DD, YYYY
		`\b(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\s+\d{1,2},?\s+\d{4}\b|` +
		// English DD Month YYYY
		`\b\d{1,2}\s+(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\s+\d{4}\b|` +
		// Multilingual Month/YY or Month/YYYY shorthand (covers "März/42",
		// "marzo/59", "Mai/24"). Slash, dash, or space separator. The
		// (?i:...) group is case-insensitive but the rest of the regex
		// stays case-sensitive.
		`\b(?i:` + monthsMulti + `)[/\- ]\d{2,4}\b|` +
		// Multilingual DD Month YYYY (covers "28 marzo 2024", "1 mars 1998")
		`\b\d{1,2}\s+(?i:` + monthsMulti + `)\s+\d{2,4}\b|` +
		// Bare time HH:MM(:SS). Stripped of am/pm — ai4privacy gold spans
		// time-only forms like "07:48" and "11:13:23". The MM and SS
		// constraints (00-59) trade some recall for sharply lower FP risk
		// on numeric ratios / verse references.
		// Accepts both zero-padded ("07:48") and bare-digit ("7:20") hours.
		`\b(?:[01]?\d|2[0-3]):[0-5]\d(?::[0-5]\d)?\b|` +
		// French / Italian / general short hour form "11h", "3h", "17h".
		// Tight constraint: 1-2 digits, lowercase 'h', word boundaries.
		// FP risk: "3h ago" — overcollects, acceptable for a redactor.
		`\b(?:[01]?\d|2[0-3])h\b`,
)

// NewDateTimeRecognizer detects DATE_TIME entities.
func NewDateTimeRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"DateTimeRecognizer",
		[]string{"DATE_TIME"},
		[]string{"*"},
		[]namedPattern{{re: dateTimeRE, score: 0.85}},
	)
}
