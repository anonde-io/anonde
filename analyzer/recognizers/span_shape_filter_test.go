package recognizers

import "testing"

// TestSpanFilterDisabledIsNoOp confirms the zero value never rejects.
func TestSpanFilterDisabledIsNoOp(t *testing.T) {
	t.Parallel()
	var f SpanFilterConfig
	for _, s := range []string{"gpt-4o", "en-US", "550e8400-e29b-41d4-a716-446655440000", "Maria Lopez"} {
		if f.rejectSpanSurface("PERSON", s) {
			t.Errorf("disabled filter rejected %q (must be no-op)", s)
		}
	}
}

// TestSpanFilterRejectsStructuralOnFuzzyTypes is the core precision claim:
// structural surfaces are rejected for the fuzzy types.
func TestSpanFilterRejectsStructuralOnFuzzyTypes(t *testing.T) {
	t.Parallel()
	f := StrictSpanFilter()
	structural := []string{
		// UUIDs
		"550e8400-e29b-41d4-a716-446655440000",
		"{550e8400-e29b-41d4-a716-446655440000}",
		"6BA7B810-9DAD-11D1-80B4-00C04FD430C8",
		// locales
		"en-US", "de_DE", "zh-Hans-CN", "pt-BR",
		// versions / semver
		"v1.2.3", "1.81.1", "2.0.0-rc1", "v3",
		// model slugs
		"gpt-4o", "gpt-3.5-turbo", "llama-2", "claude-3", "text-davinci-003",
		// hex blobs
		"deadbeefcafebabe1234", "a1b2c3d4e5f6a7b8c9d0",
		// base64 blobs
		"dGhpc2lzYXNlY3JldHRva2VuMTIz", "eyJhbGciOiJIUzI1NiJ9abc123",
		// SCREAMING_SNAKE
		"HTTP_X_FORWARDED_FOR", "API_KEY", "MAX_RETRIES",
		// dotted paths
		"com.example.service", "app.config.timeout.ms",
		// pure digit/punct (non-AGE)
		"12:34:56", "1,234.56", "----",
	}
	for _, s := range structural {
		for _, typ := range []string{"PERSON", "ORGANIZATION", "LOCATION", "NRP", "PROFESSION"} {
			if !f.rejectSpanSurface(typ, s) {
				t.Errorf("STRICT filter FAILED to reject structural %q as %s", s, typ)
			}
		}
	}
}

// TestSpanFilterKeepsRealPII is the recall-safety claim: real PII surfaces
// must never be rejected. A single false reject here is a leak.
func TestSpanFilterKeepsRealPII(t *testing.T) {
	t.Parallel()
	f := StrictSpanFilter()
	cases := []struct {
		typ, surface string
	}{
		{"PERSON", "Maria Lopez"},
		{"PERSON", "John Doe"},
		{"PERSON", "Dr. Schmidt"},
		{"PERSON", "Jean-Pierre Dubois"},
		{"PERSON", "李伟"},
		{"PERSON", "Müller"},
		{"PERSON", "O'Brien"},
		{"PERSON", "Anna-Lena Weber"},
		{"ORGANIZATION", "Acme Corp"},
		{"ORGANIZATION", "Deutsche Bank AG"},
		{"ORGANIZATION", "Universitätsklinikum Heidelberg"},
		{"ORGANIZATION", "Côte d'Or"},
		{"LOCATION", "New York"},
		{"LOCATION", "München"},
		{"LOCATION", "São Paulo"},
		{"LOCATION", "Baden-Württemberg"},
		{"NRP", "German"},
		{"NRP", "Catholic"},
		{"NRP", "Democratic Party"},
		{"PROFESSION", "software engineer"},
		{"PROFESSION", "Oberarzt"},
		{"AGE", "42"},       // numeric — must survive (AGE exempt from digit rule)
		{"AGE", "42 years"}, // must survive
		{"AGE", "thirty"},   // word age
	}
	for _, c := range cases {
		if f.rejectSpanSurface(c.typ, c.surface) {
			t.Errorf("STRICT filter WRONGLY rejected real PII %q as %s (LEAK risk)", c.surface, c.typ)
		}
	}
}

// TestSpanFilterNeverTouchesStructuredTypes guards the invariant that
// high-precision pattern types are immune even on structural surfaces:
// a UUID the model called an "id number" might be a real ID we want.
func TestSpanFilterNeverTouchesStructuredTypes(t *testing.T) {
	t.Parallel()
	f := StrictSpanFilter()
	structuredTypes := []string{
		"EMAIL_ADDRESS", "IBAN_CODE", "CREDIT_CARD", "PHONE_NUMBER",
		"US_SSN", "ID", "URL", "DATE_TIME", "POSTAL_CODE", "US_BANK_NUMBER",
		"STREET_ADDRESS", "ADDRESS",
	}
	for _, typ := range structuredTypes {
		for _, s := range []string{"550e8400-e29b-41d4-a716-446655440000", "v1.2.3", "gpt-4o"} {
			if f.rejectSpanSurface(typ, s) {
				t.Errorf("filter rejected %q as structured type %s (must never fire on structured types)", s, typ)
			}
		}
	}
}

// TestSpanFilterStoplist confirms the model-name / tech-term denylist and
// caller extensions both fire (case-insensitively).
func TestSpanFilterStoplist(t *testing.T) {
	t.Parallel()
	f := StrictSpanFilter("acmewidget", "Foobar")
	for _, s := range []string{"gpt-4o", "Claude", "GEMINI", "sonnet", "json", "acmewidget", "foobar", "FOOBAR"} {
		if !f.rejectSpanSurface("ORGANIZATION", s) {
			t.Errorf("stoplist FAILED to reject %q", s)
		}
	}
	// A non-stoplisted ordinary word must survive.
	if f.rejectSpanSurface("PERSON", "Madison") {
		t.Error("stoplist wrongly rejected ordinary name Madison")
	}
}

// TestMoneyGuardUniversal asserts the currency-amount guard on ID/POSTAL_CODE
// fires for EVERY profile that has it on (the default money-only NER profile
// AND the opt-in shape profiles), not just legal — this is what makes finance
// / clinical / chat benefit.
func TestMoneyGuardUniversal(t *testing.T) {
	t.Parallel()
	for _, f := range []SpanFilterConfig{MoneyGuardFilter(), NewSpanFilter(), StrictSpanFilter(), LegalSpanFilter(), ClinicalSpanFilter()} {
		for _, c := range []struct{ typ, s string }{
			{"ID", "8.750 EUR"}, {"ID", "2.300,00 EUR"}, {"ID", "150.000 EUR"},
			{"ID", "€42"}, {"ID", "USD 1,200"}, {"POSTAL_CODE", "8.750 EUR"},
		} {
			if !f.rejectSpanSurface(c.typ, c.s) {
				t.Errorf("money guard FAILED to reject %s %q (universal, any label set)", c.typ, c.s)
			}
		}
		// Bare digit runs (real account / case-number fragments) must survive.
		for _, c := range []struct{ typ, s string }{
			{"ID", "4496957"}, {"ID", "77"}, {"POSTAL_CODE", "10115"},
		} {
			if f.rejectSpanSurface(c.typ, c.s) {
				t.Errorf("money guard WRONGLY rejected bare %s %q (leak risk)", c.typ, c.s)
			}
		}
	}
}

// TestMoneyGuardOnlyLeavesShapesAlone asserts the default money-only profile
// does NOT run the opt-in shape rules: a model-slug PERSON survives (the bench
// proved those shapes overlap gold on PII-dense corpora).
func TestMoneyGuardOnlyLeavesShapesAlone(t *testing.T) {
	t.Parallel()
	f := MoneyGuardFilter()
	for _, c := range []struct{ typ, s string }{
		{"PERSON", "gpt-4o"}, {"PERSON", "lenoci65"}, {"LOCATION", "52159"},
		{"LOCATION", "49.8036"}, {"ID", "§ 29 ZPO"},
	} {
		if f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("money-only profile wrongly rejected %s %q (shape rules must be opt-in)", c.typ, c.s)
		}
	}
}

// TestSpanFilterDisabled asserts the escape hatch: the zero value rejects
// nothing, including the money guard.
func TestSpanFilterDisabled(t *testing.T) {
	t.Parallel()
	var f SpanFilterConfig // Enabled=false, MoneyGuard=false
	if f.Active() {
		t.Error("zero-value config must be inactive")
	}
	for _, c := range []struct{ typ, s string }{
		{"ID", "8.750 EUR"}, {"PERSON", "gpt-4o"}, {"ID", "§ 29 ZPO"},
	} {
		if f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("disabled filter wrongly rejected %s %q", c.typ, c.s)
		}
	}
}

// TestLegalSpanFilterRejectsLegalNoise confirms the legal layer adds the
// statute / exhibit ID/POSTAL_CODE rejection and the German party-role
// stoplist on top of the universal filter.
func TestLegalSpanFilterRejectsLegalNoise(t *testing.T) {
	t.Parallel()
	f := LegalSpanFilter()

	// Statute / exhibit on ID or POSTAL_CODE → rejected (legal-only).
	for _, c := range []struct{ typ, s string }{
		{"ID", "§ 779 BGB"}, {"ID", "§§ 330 ff. ZPO"}, {"ID", "§ 29 ZPO"},
		{"ID", "K3"}, {"ID", "Anlage K1"}, {"POSTAL_CODE", "§ 29 ZPO"},
		{"POSTAL_CODE", "K1"}, {"ID", "ZPO"},
	} {
		if !f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("legal filter FAILED to reject %s %q", c.typ, c.s)
		}
	}

	// German party-role words on PERSON/ORG → rejected.
	for _, c := range []struct{ typ, s string }{
		{"PERSON", "Kläger"}, {"PERSON", "Beklagte"}, {"PERSON", "Klägervertreter"},
		{"PERSON", "Bevollmächtigte"}, {"ORGANIZATION", "Partei"},
		{"ORGANIZATION", "Kaufvertrag"}, {"PERSON", "Beklagten"},
	} {
		if !f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("legal filter FAILED to reject role %s %q", c.typ, c.s)
		}
	}

	// LEAK-SAFETY: real legal IDs and names MUST survive.
	for _, c := range []struct{ typ, s string }{
		{"ID", "25 VIII 24/22"}, {"ID", "4496957"}, {"ID", "77"},
		{"ID", "5912115"}, {"POSTAL_CODE", "10115"},
		{"PERSON", "Beate Roth"}, {"ORGANIZATION", "Amtsgericht Bremen"},
	} {
		if f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("legal filter WRONGLY rejected real-PII %s %q (leak risk)", c.typ, c.s)
		}
	}
}

// TestUniversalNonNameGuardFiresUnderMoneyGuard is the precision claim for the
// always-on non-name surface guard: under the DEFAULT NER profile
// (MoneyGuardFilter, Enabled=false) a fuzzy-type span whose WHOLE surface is a
// known never-a-name token is dropped. These are the recurring GLiNER PERSON /
// ORGANIZATION false positives that the bench FP audit on openmed /
// synth_clinical / ai4privacy_en proved are pure FPs (zero gold overlap).
func TestUniversalNonNameGuardFiresUnderMoneyGuard(t *testing.T) {
	t.Parallel()
	f := MoneyGuardFilter() // Enabled=false; only the always-on tiers run.
	cases := []struct{ typ, s string }{
		// Pronouns / discourse leads mislabelled PERSON.
		{"PERSON", "Please"}, {"PERSON", "I"}, {"PERSON", "you"}, {"PERSON", "we"},
		{"PERSON", "Kindly"}, {"PERSON", "Hello"},
		// Role / relation common nouns (EN + DE). NB: clinical "patient" /
		// "Patientin" are deliberately NOT here — they are excluded from the
		// set (see TestUniversalNonNameGuardLeakSafety) to keep the openmed
		// AGE-adjacency leak floor.
		{"PERSON", "Kollege"}, {"PERSON", "Frau Kollegin"}, {"PERSON", "Großmutter"},
		{"PERSON", "student"}, {"PERSON", "child"}, {"PERSON", "client"},
		// Demographic tokens.
		{"PERSON", "Male"}, {"PERSON", "Female"},
		// Browser / user-agent tokens mislabelled PERSON or ORG.
		{"PERSON", "Mozilla"}, {"PERSON", "Gecko"}, {"PERSON", "Windows NT"},
		{"ORGANIZATION", "KHTML"}, {"ORGANIZATION", "Trident"}, {"ORGANIZATION", "AppleWebKit"},
		// Synthetic account-type phrases mislabelled ORG/PERSON.
		{"ORGANIZATION", "Savings Account"}, {"PERSON", "Investment Account"},
		// Card-brand slugs mislabelled ORG.
		{"ORGANIZATION", "diners_club"}, {"ORGANIZATION", "american_express"},
		{"ORGANIZATION", "mastercard"},
		// Case-insensitivity.
		{"PERSON", "PLEASE"}, {"ORGANIZATION", "Mozilla"},
	}
	for _, c := range cases {
		if !f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("non-name guard FAILED to reject %s %q under MoneyGuardFilter (precision FP)", c.typ, c.s)
		}
	}
}

// TestUniversalNonNameGuardLeakSafety is the recall-safety claim: the always-on
// guard must never drop a real name / org / place, must never fire on a
// multi-token span that merely contains a stoplisted word, and must never touch
// structured types. A single false reject here is a leak.
func TestUniversalNonNameGuardLeakSafety(t *testing.T) {
	t.Parallel()
	f := MoneyGuardFilter()
	keep := []struct{ typ, s string }{
		// Real names containing a stoplisted token as a substring/word.
		{"PERSON", "Patient Müller"}, {"PERSON", "Mr Patient"},
		{"PERSON", "Maria Lopez"}, {"PERSON", "John Doe"},
		// Real names that are ordinary English words but NOT in the set.
		{"PERSON", "Mark"}, {"PERSON", "Bill"}, {"PERSON", "Will"}, {"PERSON", "Madison"},
		// Deliberately-excluded role/title words that anchor gold org/job spans.
		{"PERSON", "Administrator"}, {"ORGANIZATION", "Central Tactics Administrator"},
		{"PERSON", "employee"}, {"PROFESSION", "manager"}, {"PROFESSION", "engineer"},
		// Clinical patient-role nouns deliberately excluded (openmed AGE
		// adjacency leak floor): they must survive the guard.
		{"PERSON", "patient"}, {"PERSON", "Patientin"}, {"PERSON", "Patienten"},
		// Real orgs / places.
		{"ORGANIZATION", "Deutsche Bank AG"}, {"ORGANIZATION", "Acme Corp"},
		{"LOCATION", "New York"}, {"LOCATION", "München"},
		// Structured types must be immune even on a stoplisted surface.
		{"ID", "patient"}, {"EMAIL_ADDRESS", "mozilla"}, {"URL", "discover"},
		{"DATE_TIME", "male"},
	}
	for _, c := range keep {
		if f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("non-name guard WRONGLY rejected %s %q (LEAK risk)", c.typ, c.s)
		}
	}
}

// TestUniversalNonNameGuardNeedsMoneyGuard confirms the guard is gated on the
// MoneyGuard tier: a fully-disabled (zero value) config never fires it, so the
// escape hatch still disables ALL filtering.
func TestUniversalNonNameGuardNeedsMoneyGuard(t *testing.T) {
	t.Parallel()
	var f SpanFilterConfig // MoneyGuard=false, Enabled=false
	for _, c := range []struct{ typ, s string }{
		{"PERSON", "patient"}, {"ORGANIZATION", "mozilla"}, {"PERSON", "Please"},
	} {
		if f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("disabled config wrongly rejected %s %q via non-name guard", c.typ, c.s)
		}
	}
}

// TestLegalNoiseScopedToIDTypes confirms the statute/exhibit path is
// ID/POSTAL_CODE-scoped: an exhibit-shaped PERSON is not dropped by it.
func TestLegalNoiseScopedToIDTypes(t *testing.T) {
	t.Parallel()
	f := LegalSpanFilter()
	if f.rejectSpanSurface("PERSON", "K3") {
		t.Error("legal-noise path wrongly rejected PERSON K3 (must be ID/POSTAL_CODE-scoped)")
	}
}

// TestClinicalSpanFilterRejectsClinicalNoise confirms the clinical profile
// drops the recurring PERSON / ORGANIZATION FP surface forms (patient/staff
// roles, sex markers, field labels, department names) from the meddocan_es /
// openmed FP audit, plus the productive department-context constructs.
func TestClinicalSpanFilterRejectsClinicalNoise(t *testing.T) {
	t.Parallel()
	f := ClinicalSpanFilter()

	// Stoplist: role / demographic / field-label / clinical-term surfaces.
	for _, c := range []struct{ typ, s string }{
		// Spanish (meddocan) PERSON FPs.
		{"PERSON", "paciente"}, {"PERSON", "Paciente"}, {"PERSON", "varón"},
		{"PERSON", "mujer"}, {"PERSON", "hombre"}, {"PERSON", "h"}, {"PERSON", "m"},
		{"PERSON", "fecha"}, {"PERSON", "edad"}, {"PERSON", "médico"},
		{"PERSON", "además"}, {"PERSON", "tumor"}, {"PERSON", "próstata"},
		// German (openmed) PERSON FPs.
		{"PERSON", "Patient"}, {"PERSON", "Patientin"}, {"PERSON", "Patienten"},
		{"PERSON", "Pat."}, {"PERSON", "Arzt"}, {"PERSON", "Direktor"},
		{"PERSON", "Klinikvorstand"}, {"PERSON", "Anamnese"}, {"PERSON", "Befund"},
		{"PERSON", "mit"}, {"PERSON", "keine"}, {"PERSON", "Leber"}, {"PERSON", "FOLFOX"},
		// English coverage.
		{"PERSON", "patient"}, {"PERSON", "physician"}, {"PERSON", "nurse"},
		// Lone hospital departments tagged ORGANIZATION.
		{"ORGANIZATION", "hospital"}, {"ORGANIZATION", "Urgencias"},
		{"ORGANIZATION", "nefrología"}, {"ORGANIZATION", "UCI"},
		{"ORGANIZATION", "medicina interna"}, {"ORGANIZATION", "Klinik"},
		{"ORGANIZATION", "Ambulanz"}, {"ORGANIZATION", "ICU"},
	} {
		if !f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("clinical filter FAILED to reject %s %q", c.typ, c.s)
		}
	}

	// ClinicalNoise department-context gate on ORGANIZATION.
	for _, c := range []struct{ typ, s string }{
		{"ORGANIZATION", "Servicio de Urología"},
		{"ORGANIZATION", "servicio de neurocirugía"},
		{"ORGANIZATION", "Unidad de Cuidados Intensivos"},
		{"ORGANIZATION", "unidad de nefrología"},
		{"ORGANIZATION", "Área de Urgencias"},
		{"ORGANIZATION", "Sección de Cardiología"},
		{"ORGANIZATION", "Centro de Salud"},
		{"ORGANIZATION", "centro"},
	} {
		if !f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("clinical department gate FAILED to reject %s %q", c.typ, c.s)
		}
	}
}

// TestClinicalSpanFilterLeakSafety is the recall-safety claim: the clinical
// profile must NEVER drop a real patient name, a named institution, or a
// multi-token span that merely contains a stoplisted word. A single false
// reject here is a leak.
func TestClinicalSpanFilterLeakSafety(t *testing.T) {
	t.Parallel()
	f := ClinicalSpanFilter()
	keep := []struct{ typ, s string }{
		// Real patient names — including ones containing a role word.
		{"PERSON", "Ignacio Rico Pedroza"}, {"PERSON", "Patient Müller"},
		{"PERSON", "Maria Lopez"}, {"PERSON", "Hans Leber"},
		{"PERSON", "Ana Fiebre García"},
		// Named institutions / departments with a proper name attached —
		// the department gate is anchored so these survive.
		{"ORGANIZATION", "Hospital Ramón y Cajal"},
		{"ORGANIZATION", "Centro de Salud de Vallecas"},
		{"ORGANIZATION", "Klinikum München"},
		// Real places.
		{"LOCATION", "Madrid"}, {"LOCATION", "München"},
		// Structured types are immune even on a stoplisted surface.
		{"ID", "paciente"}, {"POSTAL_CODE", "28001"}, {"EMAIL_ADDRESS", "patient"},
		{"DATE_TIME", "fecha"},
	}
	for _, c := range keep {
		if f.rejectSpanSurface(c.typ, c.s) {
			t.Errorf("clinical filter WRONGLY rejected %s %q (LEAK risk)", c.typ, c.s)
		}
	}
}

// TestClinicalNoiseScopedToOrg confirms the department-context gate is
// ORGANIZATION-only: a "servicio de ..."-shaped PERSON is not dropped by the
// gate (only by a stoplist hit, which these are not).
func TestClinicalNoiseScopedToOrg(t *testing.T) {
	t.Parallel()
	f := ClinicalSpanFilter()
	if f.rejectSpanSurface("PERSON", "Servicio de Urología") {
		t.Error("clinical-noise gate wrongly rejected PERSON (must be ORGANIZATION-scoped)")
	}
}
