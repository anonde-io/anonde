package recognizers

import (
	"testing"

	"github.com/anonde-io/anonde/analyzer"
)

// TestValidatedStructuredAllowlist_NamesMatchConstructors guards the
// validated-structured carve-out in analyzer/result.go: it instantiates each of
// the 17 checksum/validator-backed recognizers via its real constructor and
// asserts the resulting Name() is on the carve-out allowlist
// (analyzer.IsValidatedStructuredName).
//
// The allowlist keys must be Name() values, not entity types. A prior revision
// keyed the map on entity types while findings carry Name(), so 16/17 entries
// silently never matched and the carve-out was a dead no-op. Pinning the
// allowlist to live Name() values here catches a rename or wrong string at test
// time instead of quietly disabling the precision fix in production.
func TestValidatedStructuredAllowlist_NamesMatchConstructors(t *testing.T) {
	// Each entry constructs the real recognizer; .Name() is the exact value a
	// finding's RecognizerName carries, which is what the carve-out keys off.
	recs := []analyzer.EntityRecognizer{
		NewAUTFNRecognizer(),
		NewAUABNRecognizer(),
		NewAUACNRecognizer(),
		NewAUMedicareRecognizer(),
		NewINAadhaarRecognizer(),
		NewINPANRecognizer(),
		NewESNIFRecognizer(),
		NewESNIERecognizer(),
		NewITFiscalCodeRecognizer(),
		NewITVATCodeRecognizer(),
		NewPLPESELRecognizer(),
		NewFIPersonalIdentityCodeRecognizer(),
		NewKRRRNRecognizer(),
		NewSGNRICRecognizer(),
		NewUKNHSRecognizer(),
		NewDESteuerIDRecognizer(),
		NewISINRecognizer(),
	}

	seen := make(map[string]bool, len(recs))
	for _, r := range recs {
		name := r.Name()
		if seen[name] {
			t.Errorf("duplicate Name() %q in the constructor set", name)
		}
		seen[name] = true
		if !analyzer.IsValidatedStructuredName(name) {
			t.Errorf("recognizer Name()=%q is NOT on the validated-structured allowlist — "+
				"the carve-out is a silent no-op for it (keys must be Name() values, not entity types)", name)
		}
	}

	// No stray keys: the allowlist must contain exactly the constructed set, so
	// a leftover entity-type key (the original bug shape) can't linger.
	if got := analyzer.ValidatedStructuredRecognizerCount(); got != len(seen) {
		t.Errorf("validated-structured allowlist has %d keys but %d distinct constructed Name()s — "+
			"stray or missing key (possible leftover entity-type key)", got, len(seen))
	}
}
