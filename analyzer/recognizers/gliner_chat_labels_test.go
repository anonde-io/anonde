// Data-layer checks on the chat-PII label set (no live GLiNER inference in
// the test env): the chat set must EXCLUDE the noisy-in-chat labels (age /
// profession / job title / date) and INCLUDE the core chat identifiers
// (person / org / email / phone), with list↔map consistency. No build tag,
// so it runs in `go test ./analyzer/...`.

package recognizers

import "testing"

func TestChatPIILabelsExcludeNoisyChatLabels(t *testing.T) {
	excluded := []string{
		"age",
		"profession",
		"job title",
		"date",
		"date of birth",
		"hospital",
		"patient name",
		"doctor name",
		"Versicherungsnummer",
		"Geburtsdatum",
	}
	for _, label := range excluded {
		if labelInSlice(ChatPIILabels, label) {
			t.Errorf("ChatPIILabels must exclude %q (over-redacts casual chat)", label)
		}
		if _, ok := ChatPIILabelToEntity[label]; ok {
			t.Errorf("ChatPIILabelToEntity must not map %q", label)
		}
	}
}

func TestChatPIILabelsIncludeCoreChatPII(t *testing.T) {
	want := map[string]string{
		"person":       "PERSON",
		"organization": "ORGANIZATION",
		"email":        "EMAIL_ADDRESS",
		"phone number": "PHONE_NUMBER",
	}
	for label, entity := range want {
		if !labelInSlice(ChatPIILabels, label) {
			t.Errorf("ChatPIILabels must include %q", label)
		}
		if got := ChatPIILabelToEntity[label]; got != entity {
			t.Errorf("ChatPIILabelToEntity[%q] = %q, want %q", label, got, entity)
		}
	}
}

// TestChatPIILabelMapConsistency asserts every label in the list has a
// mapping and vice versa — a missing mapping silently drops a label's spans
// at result time, a defensive check against future edits drifting the two.
func TestChatPIILabelMapConsistency(t *testing.T) {
	for _, label := range ChatPIILabels {
		if _, ok := ChatPIILabelToEntity[label]; !ok {
			t.Errorf("ChatPIILabels has %q with no ChatPIILabelToEntity mapping", label)
		}
	}
	for label := range ChatPIILabelToEntity {
		if !labelInSlice(ChatPIILabels, label) {
			t.Errorf("ChatPIILabelToEntity maps %q absent from ChatPIILabels", label)
		}
	}
}

// TestClinicalPIILabelsCarryClinicalCoverage guards that the clinical set
// still carries AGE / DATE coverage — the chat curation must not regress it.
func TestClinicalPIILabelsCarryClinicalCoverage(t *testing.T) {
	for _, label := range []string{"age", "profession", "date", "date of birth", "Versicherungsnummer"} {
		if !labelInSlice(ClinicalPIILabels, label) {
			t.Errorf("ClinicalPIILabels must keep %q for clinical/HIPAA users", label)
		}
	}
}

// TestDefaultPIILabelsAreChat: the default IS the chat set — DefaultPIILabels
// must carry no AGE / PROFESSION (clinical-only).
func TestDefaultPIILabelsAreChat(t *testing.T) {
	if len(DefaultPIILabels) != len(ChatPIILabels) {
		t.Fatalf("DefaultPIILabels len=%d, want chat set len=%d", len(DefaultPIILabels), len(ChatPIILabels))
	}
	for _, noisy := range []string{"age", "profession", "date", "patient name"} {
		if labelInSlice(DefaultPIILabels, noisy) {
			t.Errorf("DefaultPIILabels (= chat) must not include %q", noisy)
		}
	}
}

// TestDefaultGLiNERLabelsAreChat: the empty-Labels fallback resolves to the
// chat set (DefaultPIILabels) — a no-label caller must get chat.
func TestDefaultGLiNERLabelsAreChat(t *testing.T) {
	got := defaultGLiNERLabels()
	if len(got) != len(ChatPIILabels) {
		t.Fatalf("defaultGLiNERLabels() len=%d, want chat set len=%d", len(got), len(ChatPIILabels))
	}
	for i, l := range ChatPIILabels {
		if got[i] != l {
			t.Errorf("defaultGLiNERLabels()[%d] = %q, want %q (chat set)", i, got[i], l)
		}
	}
	// The chat default must NOT carry the noisy clinical/AGE labels — a
	// regression to the clinical default would re-introduce over-redaction.
	for _, noisy := range []string{"age", "profession", "date", "patient name"} {
		if labelInSlice(got, noisy) {
			t.Errorf("defaultGLiNERLabels() must not include %q (would over-redact chat)", noisy)
		}
	}
	gotMap := defaultGLiNERLabelToEntity()
	if len(gotMap) != len(ChatPIILabelToEntity) {
		t.Fatalf("defaultGLiNERLabelToEntity() len=%d, want chat map len=%d", len(gotMap), len(ChatPIILabelToEntity))
	}
}

// TestEmptyConfigResolvesToChatEntities: an empty GLiNERConfig exposes the
// chat set's entity types, NOT AGE / PROFESSION (clinical-only). Runs in the
// default build via the _off.go SupportedEntities path.
func TestEmptyConfigResolvesToChatEntities(t *testing.T) {
	def := NewGLiNERRecognizer(GLiNERConfig{}).SupportedEntities()
	if containsString(def, "AGE") || containsString(def, "PROFESSION") {
		t.Errorf("empty-config GLiNER SupportedEntities() = %v; must not include AGE/PROFESSION (chat default)", def)
	}
	if !containsString(def, "PERSON") || !containsString(def, "EMAIL_ADDRESS") {
		t.Errorf("empty-config GLiNER SupportedEntities() = %v; must include core chat PII (PERSON/EMAIL_ADDRESS)", def)
	}

	// Pinning the clinical map (what GLINER_LABEL_SET=clinical and the bench
	// runner do) must restore the AGE / PROFESSION coverage.
	clin := NewGLiNERRecognizer(GLiNERConfig{
		Labels:        ClinicalPIILabels,
		LabelToEntity: ClinicalLabelToEntity,
	}).SupportedEntities()
	if !containsString(clin, "AGE") || !containsString(clin, "PROFESSION") {
		t.Errorf("clinical-config GLiNER SupportedEntities() = %v; must include AGE/PROFESSION", clin)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func labelInSlice(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
