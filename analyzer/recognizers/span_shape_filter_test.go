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
// must NEVER be rejected. A single false reject here is a leak.
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
	for _, f := range []SpanFilterConfig{MoneyGuardFilter(), NewSpanFilter(), StrictSpanFilter(), LegalSpanFilter()} {
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

// TestLegalNoiseScopedToIDTypes confirms the statute/exhibit path is
// ID/POSTAL_CODE-scoped: an exhibit-shaped PERSON is not dropped by it.
func TestLegalNoiseScopedToIDTypes(t *testing.T) {
	t.Parallel()
	f := LegalSpanFilter()
	if f.rejectSpanSurface("PERSON", "K3") {
		t.Error("legal-noise path wrongly rejected PERSON K3 (must be ID/POSTAL_CODE-scoped)")
	}
}
