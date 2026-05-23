package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// ISIN — International Securities Identification Number (ISO 6166).
// 12 alphanumeric chars: 2-letter ISO country code + 9 alphanumeric
// NSIN + 1 numeric Luhn check digit. Examples from the finance_de
// brokerage-statement corpus:
//
//	DE0008404005     Allianz SE
//	DE000BASF111     BASF SE
//	IE00B4L5Y983     iShares Core MSCI World
//	LU0290358497     Lyxor STOXX Europe 600
//
// FP risk on random 12-char ALL-CAPS strings is real, so we gate on the
// country-code position: it must be one of ~40 ISO 3166-1 codes that
// have active securities-listing markets. That keeps the regex blind to
// e.g. "ABCDEFGHIJK1" while catching every ISIN actually printed on a
// real broker statement.

var isinRE = regexp.MustCompile(
	// 4-letter country code position + 9 alphanumeric body + 1 digit.
	// The country-code list mirrors the major ISIN-issuing jurisdictions
	// from FactSet's exchange map.
	`\b(?:` +
		`DE|AT|CH|LI|FR|IT|ES|PT|NL|BE|LU|GB|IE|US|CA|` +
		`DK|SE|NO|FI|IS|PL|CZ|SK|HU|RO|BG|GR|HR|SI|EE|LT|LV|CY|MT|TR|` +
		`JP|CN|HK|SG|AU|NZ|KR|IN|BR|MX|ZA|IL|AE|SA|KY|BM` +
		`)[A-Z0-9]{9}\d\b`,
)

// isinValidate performs the Luhn-style "ISIN check digit" verification
// described in ISO 6166. Each character is expanded to its decimal
// value (digits stay 0-9, letters use A=10, B=11, …, Z=35), digits
// are concatenated, then a modulus-10 Luhn-style check is applied.
// Returns true if the trailing check digit is consistent with the
// prefix. Used to keep precision high on the bare-shape regex.
func isinValidate(s string) bool {
	if len(s) != 12 {
		return false
	}
	digits := make([]int, 0, 24)
	for i := 0; i < 12; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = append(digits, int(c-'0'))
		case c >= 'A' && c <= 'Z':
			v := int(c-'A') + 10
			digits = append(digits, v/10, v%10)
		default:
			return false
		}
	}
	sum := 0
	for i, d := range digits {
		// Double every second digit, starting from the rightmost-1.
		if (len(digits)-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// ISINRecognizer detects ISIN codes with check-digit validation.
type ISINRecognizer struct{}

// NewISINRecognizer constructs the recognizer.
func NewISINRecognizer() *ISINRecognizer { return &ISINRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *ISINRecognizer) Name() string { return "ISINRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
// ISIN is canonicalised to ID in the label map (anonde.ID -> ID).
func (r *ISINRecognizer) SupportedEntities() []string { return []string{"ID"} }

// SupportedLanguages reports `*` — ISIN format is language-independent.
func (r *ISINRecognizer) SupportedLanguages() []string { return []string{"*"} }

// Analyze scans text for ISIN candidates and validates each via the
// Luhn-style ISO 6166 check digit. Candidates that don't validate are
// dropped to keep precision high.
func (r *ISINRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	for _, m := range isinRE.FindAllStringIndex(text, -1) {
		candidate := text[m[0]:m[1]]
		if !isinValidate(candidate) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.95,
			EntityType: "ID", RecognizerName: r.Name(),
		})
	}
	return out, nil
}
