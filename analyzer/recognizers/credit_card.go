package recognizers

import (
	"context"
	"regexp"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

var creditCardRE = regexp.MustCompile(
	`\b(?:4[0-9]{12}(?:[0-9]{3})?` + // Visa
		`|5[1-5][0-9]{14}` + // MasterCard
		`|3[47][0-9]{13}` + // Amex
		`|3(?:0[0-5]|[68][0-9])[0-9]{11}` + // Diners
		`|6(?:011|5[0-9]{2})[0-9]{12}` + // Discover
		`|(?:2131|1800|35\d{3})\d{11}` + // JCB
		`|(?:\d{4}[ \-]){3}\d{4}` + // 4-4-4-4 with separators
		`|(?:\d{4}[ \-]){2}\d{6}` + // 4-4-6 (Amex)
		`|\d{13,19}` + // bare 13–19 digits; Luhn validates
		`)\b`,
)

// CreditCardRecognizer detects CREDIT_CARD entities and validates via Luhn.
type CreditCardRecognizer struct{}

func NewCreditCardRecognizer() *CreditCardRecognizer { return &CreditCardRecognizer{} }

func (c *CreditCardRecognizer) Name() string                 { return "CreditCardRecognizer" }
func (c *CreditCardRecognizer) SupportedEntities() []string  { return []string{"CREDIT_CARD"} }
func (c *CreditCardRecognizer) SupportedLanguages() []string { return []string{"*"} }

// ContextKeywords implements analyzer.ContextProvider.
func (c *CreditCardRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{
		"CREDIT_CARD": {"credit", "card", "cc", "visa", "mastercard", "amex", "american express", "discover", "diners", "jcb", "payment"},
	}
}

func (c *CreditCardRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult
	for _, m := range creditCardRE.FindAllStringIndex(text, -1) {
		raw := strings.ReplaceAll(text[m[0]:m[1]], " ", "")
		raw = strings.ReplaceAll(raw, "-", "")
		score := 0.3
		if luhn(raw) {
			score = 1.0
		}
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: score,
			EntityType: "CREDIT_CARD", RecognizerName: "CreditCardRecognizer",
		})
	}
	return results, nil
}

func luhn(number string) bool {
	sum := 0
	nDigits := len(number)
	parity := nDigits % 2
	for i, ch := range number {
		digit := int(ch - '0')
		if digit < 0 || digit > 9 {
			return false
		}
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}
