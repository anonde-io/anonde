package recognizers

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

var ibanRE = regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{4,30}\b`)

// IBANRecognizer detects IBAN_CODE entities with MOD-97 validation.
type IBANRecognizer struct{}

func NewIBANRecognizer() *IBANRecognizer { return &IBANRecognizer{} }

func (r *IBANRecognizer) Name() string                 { return "IBANRecognizer" }
func (r *IBANRecognizer) SupportedEntities() []string  { return []string{"IBAN_CODE"} }
func (r *IBANRecognizer) SupportedLanguages() []string { return []string{"*"} }

// ContextKeywords implements analyzer.ContextProvider.
func (r *IBANRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{
		"IBAN_CODE": {"iban", "bank", "account", "wire", "swift", "transfer", "bic"},
	}
}

func (r *IBANRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult
	for _, m := range ibanRE.FindAllStringIndex(text, -1) {
		candidate := strings.ReplaceAll(text[m[0]:m[1]], " ", "")
		score := 0.5
		if validateIBAN(candidate) {
			score = 1.0
		}
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: score,
			EntityType: "IBAN_CODE", RecognizerName: "IBANRecognizer",
		})
	}
	return results, nil
}

func validateIBAN(iban string) bool {
	if len(iban) < 5 || len(iban) > 34 {
		return false
	}
	rearranged := iban[4:] + iban[:4]
	var numeric strings.Builder
	for _, ch := range rearranged {
		if ch >= '0' && ch <= '9' {
			numeric.WriteRune(ch)
		} else if ch >= 'A' && ch <= 'Z' {
			numeric.WriteString(fmt.Sprintf("%d", int(ch-'A')+10))
		} else {
			return false
		}
	}
	n := new(big.Int)
	n.SetString(numeric.String(), 10)
	mod := new(big.Int)
	mod.Mod(n, big.NewInt(97))
	return mod.Int64() == 1
}
