package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// US ZIP code recognizer. The US Postal Service ZIP layout is 5 digits
// or ZIP+4 (5 digits, hyphen, 4 digits). Bare 5-digit shapes are
// ambiguous in raw prose — they collide with years, dollar amounts,
// lab values, document numbers — so this recognizer scores a bare
// 5-digit match LOW (0.55), enough to redact when no contradictory
// stronger signal fires but not enough to win against e.g. a DATE
// context match. ZIP+4 ("28940-9232") is rare enough as an accidental
// shape that it scores 0.85.
//
// Context-aware boosting via ContextKeywords lifts the bare 5-digit
// score when "ZIP", "zip code", "postal code", a US state abbreviation,
// or "address" is in the context window.

var (
	// ZIP+4: 5 digits, hyphen, 4 digits.
	usZip4RE = regexp.MustCompile(`\b\d{5}-\d{4}\b`)

	// Bare 5-digit shape. Score is intentionally low.
	usZip5RE = regexp.MustCompile(`\b\d{5}\b`)
)

// USZipRecognizer detects US ZIP codes.
type USZipRecognizer struct{}

// NewUSZipRecognizer constructs the recognizer.
func NewUSZipRecognizer() *USZipRecognizer { return &USZipRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *USZipRecognizer) Name() string { return "USZipRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
// POSTAL_CODE is folded to LOCATION by ai4privacy and similar
// English corpora via --fold-parity-labels in the bench harness.
func (r *USZipRecognizer) SupportedEntities() []string { return []string{"POSTAL_CODE"} }

// SupportedLanguages reports en — bare 5-digit shapes are too noisy in
// German clinical text (lab values, document numbers) to enable there.
func (r *USZipRecognizer) SupportedLanguages() []string { return []string{"en"} }

// ContextKeywords boosts a bare 5-digit match when one of the address
// / postal cues appears in the surrounding context window.
func (r *USZipRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{
		"POSTAL_CODE": {
			"zip", "zip code", "postal code", "postal-code",
			"address", "street", "ave", "avenue", "boulevard", "blvd",
			"mailing", "shipping", "billing",
		},
	}
}

// Analyze scans text for US ZIP shapes.
func (r *USZipRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	// ZIP+4 first — emit at the higher score and let the conflict
	// resolver drop overlapping bare-5 matches at score 0.55.
	for _, m := range usZip4RE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.85,
			EntityType: "POSTAL_CODE", RecognizerName: r.Name(),
		})
	}
	for _, m := range usZip5RE.FindAllStringIndex(text, -1) {
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.55,
			EntityType: "POSTAL_CODE", RecognizerName: r.Name(),
		})
	}
	return out, nil
}
