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
