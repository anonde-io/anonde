// Data-layer checks on the finance + legal label sets (no live GLiNER
// inference in the test env): presence of the vertical-defining labels,
// absence of the dropped labels, and list↔map consistency (a missing mapping
// silently drops spans). No build tag, so it runs in `go test ./analyzer/...`.

package recognizers

import "testing"

func TestFinancePIILabelsIncludeStructuredFinanceIDs(t *testing.T) {
	want := map[string]string{
		"person":              "PERSON",
		"account holder":      "PERSON",
		"organization":        "ORGANIZATION",
		"email":               "EMAIL_ADDRESS",
		"phone number":        "PHONE_NUMBER",
		"bank account number": "US_BANK_NUMBER",
		"routing number":      "ID",
		"iban":                "IBAN_CODE",
		"swift code":          "ID",
		"bic":                 "ID",
		"credit card":         "CREDIT_CARD",
		"cvv":                 "CREDIT_CARD",
		"ssn":                 "US_SSN",
		"itin":                "US_ITIN",
		"ein":                 "ID",
		"transaction id":      "ID",
	}
	for label, entity := range want {
		if !labelInSlice(FinancePIILabels, label) {
			t.Errorf("FinancePIILabels must include %q", label)
		}
		if got := FinancePIILabelToEntity[label]; got != entity {
			t.Errorf("FinancePIILabelToEntity[%q] = %q, want %q", label, got, entity)
		}
	}
}

func TestFinancePIILabelsExcludeNoisyLabels(t *testing.T) {
	// Finance docs have no canonical use for these and they over-fire.
	for _, label := range []string{"profession", "job title", "age", "patient name", "hospital"} {
		if labelInSlice(FinancePIILabels, label) {
			t.Errorf("FinancePIILabels must exclude %q", label)
		}
		if _, ok := FinancePIILabelToEntity[label]; ok {
			t.Errorf("FinancePIILabelToEntity must not map %q", label)
		}
	}
}

func TestFinancePIILabelMapConsistency(t *testing.T) {
	for _, label := range FinancePIILabels {
		if _, ok := FinancePIILabelToEntity[label]; !ok {
			t.Errorf("FinancePIILabels has %q with no FinancePIILabelToEntity mapping", label)
		}
	}
	for label := range FinancePIILabelToEntity {
		if !labelInSlice(FinancePIILabels, label) {
			t.Errorf("FinancePIILabelToEntity maps %q absent from FinancePIILabels", label)
		}
	}
}

func TestLegalPIILabelsIncludeLegalIDsAndKeepDates(t *testing.T) {
	want := map[string]string{
		"person":          "PERSON",
		"attorney":        "PERSON",
		"plaintiff":       "PERSON",
		"defendant":       "PERSON",
		"court name":      "ORGANIZATION",
		"email":           "EMAIL_ADDRESS",
		"phone number":    "PHONE_NUMBER",
		"address":         "ADDRESS",
		"date":            "DATE_TIME",
		"date of birth":   "DATE_TIME",
		"case number":     "ID",
		"docket number":   "ID",
		"matter number":   "ID",
		"contract number": "ID",
		"bar number":      "ID",
	}
	for label, entity := range want {
		if !labelInSlice(LegalPIILabels, label) {
			t.Errorf("LegalPIILabels must include %q", label)
		}
		if got := LegalPIILabelToEntity[label]; got != entity {
			t.Errorf("LegalPIILabelToEntity[%q] = %q, want %q", label, got, entity)
		}
	}
}

// TestLegalPIIKeepsDatesUnlikeChat is the load-bearing distinction between the
// legal and chat sets: legal documents are date-sensitive, so date / DOB MUST
// be retained even though the chat set drops them.
func TestLegalPIIKeepsDatesUnlikeChat(t *testing.T) {
	for _, label := range []string{"date", "date of birth"} {
		if !labelInSlice(LegalPIILabels, label) {
			t.Errorf("LegalPIILabels must keep %q (legal docs are date-sensitive)", label)
		}
		if labelInSlice(ChatPIILabels, label) {
			t.Errorf("sanity: ChatPIILabels was expected to drop %q", label)
		}
	}
}

func TestLegalPIILabelsExcludeFinanceAndClinicalNoise(t *testing.T) {
	for _, label := range []string{"profession", "job title", "age", "cvv", "credit card", "iban", "hospital", "patient name"} {
		if labelInSlice(LegalPIILabels, label) {
			t.Errorf("LegalPIILabels must exclude %q", label)
		}
		if _, ok := LegalPIILabelToEntity[label]; ok {
			t.Errorf("LegalPIILabelToEntity must not map %q", label)
		}
	}
}

func TestLegalPIILabelMapConsistency(t *testing.T) {
	for _, label := range LegalPIILabels {
		if _, ok := LegalPIILabelToEntity[label]; !ok {
			t.Errorf("LegalPIILabels has %q with no LegalPIILabelToEntity mapping", label)
		}
	}
	for label := range LegalPIILabelToEntity {
		if !labelInSlice(LegalPIILabels, label) {
			t.Errorf("LegalPIILabelToEntity maps %q absent from LegalPIILabels", label)
		}
	}
}
