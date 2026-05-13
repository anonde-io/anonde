package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// US bank account numbers are typically 8-17 digits. Because that pattern
// also matches credit cards, phone numbers, IDs, and arbitrary digit runs,
// emission is GATED on a nearby anchoring keyword. Without the gate the
// recognizer would flood every long-digit string with a US_BANK_NUMBER
// candidate at score 0.3, tying with the CREDIT_CARD bare fallback and
// producing non-deterministic resolutions.
var usBankRE = regexp.MustCompile(`\b\d{8,17}\b`)

const (
	usBankContextWindowChars = 60
	usBankScore              = 0.3
)

var usBankContextKeywords = []string{
	"bank", "account", "checking", "savings", "routing", "ach", "wire",
}

// USBankRecognizer detects US_BANK_NUMBER entities only when an anchoring
// banking keyword appears within usBankContextWindowChars of the digit run.
type USBankRecognizer struct{}

// NewUSBankRecognizer constructs the context-gated US bank account recognizer.
func NewUSBankRecognizer() *USBankRecognizer { return &USBankRecognizer{} }

func (r *USBankRecognizer) Name() string                 { return "USBankRecognizer" }
func (r *USBankRecognizer) SupportedEntities() []string  { return []string{"US_BANK_NUMBER"} }
func (r *USBankRecognizer) SupportedLanguages() []string { return []string{"en"} }

// ContextKeywords keeps the same keywords available to the analyzer's
// context-keyword score enhancer so the score can be lifted further when
// the boost stage runs (Presidio-compatible behaviour).
func (r *USBankRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{"US_BANK_NUMBER": usBankContextKeywords}
}

func (r *USBankRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	lower := strings.ToLower(text)
	n := len(text)
	var out []analyzer.RecognizerResult
	for _, m := range usBankRE.FindAllStringIndex(text, -1) {
		start, end := m[0], m[1]
		windowStart := start - usBankContextWindowChars
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := end + usBankContextWindowChars
		if windowEnd > n {
			windowEnd = n
		}
		before := lower[windowStart:start]
		after := lower[end:windowEnd]
		if !hasContextKeyword(before, usBankContextKeywords) && !hasContextKeyword(after, usBankContextKeywords) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          start,
			End:            end,
			Score:          usBankScore,
			EntityType:     "US_BANK_NUMBER",
			RecognizerName: "USBankRecognizer",
		})
	}
	return out, nil
}

// hasContextKeyword reports whether any whole-word keyword appears in text.
// Mirrors analyzer.matchWord so the gating logic stays equivalent to the
// engine's context-enhancement check.
func hasContextKeyword(text string, keywords []string) bool {
	if text == "" {
		return false
	}
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		idx := 0
		for idx < len(text) {
			i := strings.Index(text[idx:], kw)
			if i < 0 {
				break
			}
			pos := idx + i
			leftOK := pos == 0 || !isWordByte(text[pos-1])
			rightOK := pos+len(kw) == len(text) || !isWordByte(text[pos+len(kw)])
			if leftOK && rightOK {
				return true
			}
			idx = pos + 1
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
