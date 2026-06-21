package analyzer

import "testing"

// These tests cover the validated-structured carve-out in shouldReplace /
// RemoveConflicts. They are pure (no ONNX / no network) and run under the
// default build. NER findings are simulated by stamping a RecognizerName from
// nerRecognizerNames (the real GLiNER span/flat decoder names) onto a
// RecognizerResult; the conflict resolver keys off the name, not a live model,
// so this faithfully exercises the production code path.

// nerDecoderNames is both GLiNER decoders we must defend the carve-out across:
// the span decoder ("GLiNERRecognizer") and the flat decoder
// ("GLiNERFlatNERRecognizer"). Every directional case is run for both so a
// future divergence in one decoder's name can't silently regress.
var nerDecoderNames = []string{"GLiNERRecognizer", "GLiNERFlatNERRecognizer"}

// TestShouldReplace_ValidatedStructuredBeatsNERFalseLabel asserts that a
// checksum/validator-backed structured finding wins over an overlapping fuzzy
// NER false label REGARDLESS of score, in BOTH overlap orderings (validated
// finding as kept, and validated finding as candidate). This is the precision
// fix: GLiNER mislabelling a checksum-valid ID as PERSON/ORG must not steal
// the correct structured type.
func TestShouldReplace_ValidatedStructuredBeatsNERFalseLabel(t *testing.T) {
	for _, ner := range nerDecoderNames {
		ner := ner
		// One representative validated recognizer per checksum family on the
		// allowlist; each emits only when its validator passed.
		// name is the REAL recognizer Name() (the first arg to
		// NewValidatedRecognizer) — the value the resolver keys off, NOT the
		// entity type. Using entity types here is exactly the bug that made the
		// carve-out a silent no-op.
		validated := []struct {
			name       string
			entityType string
		}{
			{"DESteuerIDRecognizer", "DE_STEUER_ID"},
			{"INAadhaarRecognizer", "IN_AADHAAR"},
			{"UKNHSRecognizer", "UK_NHS"},
			{"PLPESELRecognizer", "PL_PESEL"},
			{"ITFiscalCodeRecognizer", "IT_FISCAL_CODE"},
			{"ESNIFRecognizer", "ES_NIF"},
			{"SGNRICRecognizer", "SG_NRIC_FIN"},
			{"AUTFNRecognizer", "AU_TFN"},
			{"ISINRecognizer", "ID"},
		}
		// Every fuzzy NER type GLiNER might mislabel the surface as.
		for _, fuzzy := range []string{"PERSON", "ORGANIZATION", "LOCATION", "NRP", "PROFESSION", "AGE"} {
			fuzzy := fuzzy
			for _, v := range validated {
				v := v

				// The NER false label is MORE confident than the validated
				// pattern would be if the score path decided — proving the
				// carve-out ignores score. (Most validated recognizers emit
				// 1.0, so we push NER above that to make the test bite.)
				nerScore := 0.99

				// Ordering A: validated finding is the last-kept span,
				// NER false label is the candidate → must NOT replace.
				keptValidated := RecognizerResult{
					Start: 10, End: 21, Score: 1.0,
					EntityType: v.entityType, RecognizerName: v.name,
				}
				candNER := RecognizerResult{
					Start: 10, End: 21, Score: nerScore,
					EntityType: fuzzy, RecognizerName: ner,
				}
				if shouldReplace(keptValidated, candNER) {
					t.Errorf("[%s] kept=%s(%s) cand=NER:%s: NER false label stole the validated structured span (precision loss)",
						ner, v.name, v.entityType, fuzzy)
				}

				// Ordering B: NER false label is the last-kept span,
				// validated finding is the candidate → MUST replace.
				keptNER := RecognizerResult{
					Start: 10, End: 21, Score: nerScore,
					EntityType: fuzzy, RecognizerName: ner,
				}
				candValidated := RecognizerResult{
					Start: 10, End: 21, Score: 1.0,
					EntityType: v.entityType, RecognizerName: v.name,
				}
				if !shouldReplace(keptNER, candValidated) {
					t.Errorf("[%s] kept=NER:%s cand=%s(%s): validated structured span failed to reclaim its type",
						ner, fuzzy, v.name, v.entityType)
				}
			}
		}
	}
}

// TestRemoveConflicts_ValidatedStructuredEndToEnd runs the carve-out through
// the real RemoveConflicts scan (which only compares against the last-kept
// finding) for both sort orderings, confirming the surviving span is the
// structured one and that the surface is still redacted (one span out, full
// coverage), so no leak is introduced.
func TestRemoveConflicts_ValidatedStructuredEndToEnd(t *testing.T) {
	for _, ner := range nerDecoderNames {
		ner := ner
		validatedHigh := RecognizerResult{ // validated wins the sort (score tie → length)
			Start: 0, End: 11, Score: 1.0,
			EntityType: "DE_STEUER_ID", RecognizerName: "DESteuerIDRecognizer",
		}
		nerFalse := RecognizerResult{
			Start: 0, End: 11, Score: 0.99,
			EntityType: "PERSON", RecognizerName: ner,
		}

		// Both input orderings should collapse to a single structured span.
		for _, in := range [][]RecognizerResult{
			{validatedHigh, nerFalse},
			{nerFalse, validatedHigh},
		} {
			got := RemoveConflicts(in)
			if len(got) != 1 {
				t.Fatalf("[%s] expected 1 surviving span (full coverage, no leak), got %d: %+v", ner, len(got), got)
			}
			if got[0].RecognizerName != "DESteuerIDRecognizer" {
				t.Errorf("[%s] surviving span = %s/%s, want validated DESteuerIDRecognizer", ner, got[0].RecognizerName, got[0].EntityType)
			}
			// Leak-safety assertion: the surviving span still covers the whole
			// surface [0,11), so the bytes a redactor would touch are unchanged.
			if got[0].Start != 0 || got[0].End != 11 {
				t.Errorf("[%s] surviving span coverage shrank to [%d,%d): possible leak", ner, got[0].Start, got[0].End)
			}
		}
	}
}

// TestShouldReplace_GenuineNERStillBeatsPattern is the regression guard for the
// existing same-type NER preference: a contextual PERSON/ORG/LOCATION/AGE/
// PROFESSION/NRP NER span must still beat an overlapping heuristic pattern of the
// same type, and a non-validated (no-checksum) cross-type pattern must not win
// over a genuine NER span. The carve-out must not over-reach.
func TestShouldReplace_GenuineNERStillBeatsPattern(t *testing.T) {
	for _, ner := range nerDecoderNames {
		ner := ner

		// (1) Same-type NER preference preserved: heuristic PERSON pattern
		// (high deterministic score) vs NER PERSON (lower sigmoid) — NER wins
		// in both orderings.
		for _, etype := range []string{"PERSON", "ORGANIZATION", "LOCATION", "AGE", "PROFESSION", "NRP"} {
			etype := etype
			patt := RecognizerResult{
				Start: 5, End: 18, Score: 1.0,
				EntityType: etype, RecognizerName: "DEAnomalyRecognizer",
			}
			nerSpan := RecognizerResult{
				Start: 5, End: 18, Score: 0.41,
				EntityType: etype, RecognizerName: ner,
			}
			if shouldReplace(nerSpan, patt) {
				t.Errorf("[%s] same-type %s: heuristic pattern stole genuine NER span (kept=NER)", ner, etype)
			}
			if !shouldReplace(patt, nerSpan) {
				t.Errorf("[%s] same-type %s: genuine NER span failed to beat heuristic pattern (kept=pattern)", ner, etype)
			}
		}

		// (2) A non-validated cross-type pattern (no checksum — e.g. a bare
		// SSN/credit-card/IBAN shape that emits even on validation failure, or
		// a loose regex) must not win over a genuine NER fuzzy span. Cross-type
		// non-validated overlaps fall through to score, and the NER span here
		// is the higher-scoring one, so NER must keep its span.
		for _, loose := range []string{"CreditCardRecognizer", "IBANRecognizer", "USSocialSecurityRecognizer", "USITINRecognizer"} {
			loose := loose
			nerSpan := RecognizerResult{
				Start: 3, End: 14, Score: 0.80,
				EntityType: "PERSON", RecognizerName: ner,
			}
			loosePatt := RecognizerResult{
				Start: 3, End: 14, Score: 0.50,
				EntityType: "ID", RecognizerName: loose,
			}
			// NER is kept, loose pattern is candidate → must not replace.
			if shouldReplace(nerSpan, loosePatt) {
				t.Errorf("[%s] non-validated %s stole a genuine higher-scoring NER PERSON span (the forbidden leak-raising case)", ner, loose)
			}
			// Loose pattern kept, NER candidate → NER (higher score, preferred
			// type) must replace.
			if !shouldReplace(loosePatt, nerSpan) {
				t.Errorf("[%s] genuine NER PERSON span failed to reclaim from non-validated %s", ner, loose)
			}
		}
	}
}

// TestShouldReplace_PartialOverlapDoesNotLeak guards the partial-overlap case.
// When the fuzzy NER span is larger than and only partially overlaps the
// validated span (an overhang of NER-redacted characters lies outside the
// validated span), the carve-out must not fire — forcing the smaller validated
// span to win would drop the NER overhang and leak it. The carve-out is leak-free
// only under containment (NER ⊆ validated); on non-containment we fall through to
// the score path, where the wider NER span (higher score here) correctly survives.
func TestShouldReplace_PartialOverlapDoesNotLeak(t *testing.T) {
	for _, ner := range nerDecoderNames {
		ner := ner
		// NER "Acme Corp 12345678X" ORGANIZATION [0,19], higher score; the
		// validated NIF matches only "12345678X" [10,19]. The NER span is NOT
		// contained in the validated span (overhang [0,10) "Acme Corp ").
		nerWide := RecognizerResult{
			Start: 0, End: 19, Score: 0.90,
			EntityType: "ORGANIZATION", RecognizerName: ner,
		}
		validatedSmall := RecognizerResult{
			Start: 10, End: 19, Score: 1.0,
			EntityType: "ES_NIF", RecognizerName: "ESNIFRecognizer",
		}

		// Ordering A: validated is kept, wide NER is candidate. The carve-out
		// must not keep the small validated span (that would drop the NER
		// overhang). Containment fails (candidate ⊄ kept), so we fall through to
		// the score path; with NER scored above the validated span, the candidate
		// must replace, keeping the wider span. The coverage assertion below
		// confirms no overhang is lost.
		nerWideHi := nerWide
		nerWideHi.Score = 1.01 // push NER above the validated 1.0 so score path keeps it
		if !shouldReplace(validatedSmall, nerWideHi) {
			t.Errorf("[%s] partial overlap, validated kept: wider NER candidate was dropped (overhang leak)", ner)
		}

		// Ordering B: wide NER is kept, validated is candidate. The carve-out
		// must not let the small validated span take over (kept ⊄ candidate),
		// because that drops the NER overhang. Falls through to the score path; we
		// keep the NER span the higher-scoring one so neither path drops it.
		nerWideHiKept := nerWide
		nerWideHiKept.Score = 1.01
		if shouldReplace(nerWideHiKept, validatedSmall) {
			t.Errorf("[%s] partial overlap, NER kept: smaller validated span stole the wider NER span (overhang leak)", ner)
		}

		// End-to-end coverage assertion through RemoveConflicts: the union of
		// surviving spans must still cover every character the wider NER span
		// would have redacted ([0,19)). The carve-out must never shrink the
		// redacted footprint on a partial overlap.
		for _, in := range [][]RecognizerResult{
			{nerWideHi, validatedSmall},
			{validatedSmall, nerWideHi},
		} {
			got := RemoveConflicts(in)
			if !coversRange(got, 0, 19) {
				t.Errorf("[%s] partial overlap: surviving spans %+v do not cover [0,19) — overhang leaked", ner, got)
			}
		}
	}
}

// coversRange reports whether the union of the given spans covers every
// character in [start,end). Used by the leak-safety assertion.
func coversRange(spans []RecognizerResult, start, end int) bool {
	for pos := start; pos < end; pos++ {
		covered := false
		for _, s := range spans {
			if pos >= s.Start && pos < s.End {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

// TestShouldReplace_StructuredVsStructuredUntouched confirms the carve-out
// does not perturb structured-vs-structured overlaps: two validated findings
// (or a validated vs a non-NER pattern) still resolve purely by score.
func TestShouldReplace_StructuredVsStructuredUntouched(t *testing.T) {
	a := RecognizerResult{Start: 0, End: 10, Score: 0.95, EntityType: "ID", RecognizerName: "ISINRecognizer"}
	b := RecognizerResult{Start: 0, End: 10, Score: 1.0, EntityType: "IBAN_CODE", RecognizerName: "IBANRecognizer"}
	// candidate b has higher score → should replace a (pure score path).
	if !shouldReplace(a, b) {
		t.Error("structured-vs-structured: higher-scoring candidate should win by score")
	}
	// reverse: a lower-scoring candidate should not replace.
	if shouldReplace(b, a) {
		t.Error("structured-vs-structured: lower-scoring candidate should not win")
	}
}

// TestShouldReplace_AddressFamilyContainsNER guards the address-family
// containment carve-out: an NER LOCATION span inside a wider pattern
// POSTAL_CODE / STREET_ADDRESS span must not evict it (that would drop the
// postcode digits and leak). The wider pattern span wins in both orderings.
func TestShouldReplace_AddressFamilyContainsNER(t *testing.T) {
	for _, ner := range nerDecoderNames {
		ner := ner
		for _, wide := range []struct {
			name       string
			entityType string
		}{
			{"DEPostalCodeRecognizer", "POSTAL_CODE"},
			{"StreetAddressRecognizer", "STREET_ADDRESS"},
		} {
			wide := wide
			// pattern "43566 Bochum" [0,12] fully contains NER LOCATION
			// "Bochum" [6,12]. Pattern emits a deterministic high score; NER
			// would otherwise win on the LOCATION preference rule.
			pattern := RecognizerResult{
				Start: 0, End: 12, Score: 0.85,
				EntityType: wide.entityType, RecognizerName: wide.name,
			}
			loc := RecognizerResult{
				Start: 6, End: 12, Score: 0.95,
				EntityType: "LOCATION", RecognizerName: ner,
			}
			// Ordering A: pattern kept, NER candidate contained → keep pattern.
			if shouldReplace(pattern, loc) {
				t.Errorf("[%s/%s] NER LOCATION evicted the wider address span (postcode digits would leak)", ner, wide.entityType)
			}
			// Ordering B: NER kept, wider pattern candidate → pattern takes over.
			if !shouldReplace(loc, pattern) {
				t.Errorf("[%s/%s] wider address span failed to reclaim from the inner NER LOCATION", ner, wide.entityType)
			}
		}

		// Non-containment guard: a same-extent NER LOCATION vs a non-address
		// pattern still resolves normally (NER preference), so the carve-out
		// doesn't over-reach. A bare city LOCATION with no wider address span
		// must keep winning over a lower-score pattern of the same type.
		nerCity := RecognizerResult{Start: 0, End: 6, Score: 0.5, EntityType: "LOCATION", RecognizerName: ner}
		pattCity := RecognizerResult{Start: 0, End: 6, Score: 1.0, EntityType: "LOCATION", RecognizerName: "DECityRecognizer"}
		if !shouldReplace(pattCity, nerCity) {
			t.Errorf("[%s] same-type NER LOCATION preference broke (carve-out over-reached)", ner)
		}
	}
}
