package recognizers

import (
	"context"
	"strings"
	"testing"
)

// TestIsStructuralSurfaceDropsMachineTokens is the precision claim for the
// shared helper used by the heuristic PERSON/ORG pattern recognizers: a
// surface whose WHOLE form is a machine token is structural (never a name).
// These mirror the real false-positive sources from the agent leak-map
// dashboard (UUIDs, snake_case field keys, model slugs, hex/base64 IDs).
func TestIsStructuralSurfaceDropsMachineTokens(t *testing.T) {
	t.Parallel()
	structural := []string{
		// UUIDs (the path-UUID shape on /api/issues/{uuid}).
		"1bdf80a8-2cb1-45c6-ac96-2ca0deadbeef",
		"550e8400-e29b-41d4-a716-446655440000",
		"{550e8400-e29b-41d4-a716-446655440000}",
		// snake_case / camel-ish JSON field-key tokens.
		"conversation_id", "company_id", "issue_id", "created_at",
		"max_retries", "user_uuid", "api_key", "x_request_id",
		// SCREAMING_SNAKE.
		"API_KEY", "HTTP_X_FORWARDED_FOR", "MAX_RETRIES",
		// model slugs / RPC tokens.
		"gpt-4o-mini", "gpt-4o", "gpt-3.5-turbo", "text-davinci-003",
		"claude-3", "llama-2",
		// dotted paths / hostnames / package paths.
		"com.example.service", "app.config.timeout.ms", "api.openai.com",
		// hex blobs.
		"a1b2c3d4e5f6a1b2", "deadbeefcafebabe1234",
		// base64 / base64url blobs (>=24, has digit/symbol).
		"dGhpc2lzYXNlY3JldHRva2VuMTIz", "eyJhbGciOiJIUzI1NiJ9abc123",
		// locale tags / semver.
		"en-US", "de_DE", "zh-Hans-CN", "v1.2.3", "2.0.0-rc1",
	}
	for _, s := range structural {
		if !isStructuralSurface(s) {
			t.Errorf("isStructuralSurface(%q) = false, want true (structural FP source)", s)
		}
	}
}

// TestIsStructuralSurfaceKeepsRealNames is the LEAK-SAFETY claim: a plausible
// human name / org / place — including short single tokens and the reversible
// First_Last(digits) username gold shape — is NEVER structural. A single false
// positive here is a leak.
func TestIsStructuralSurfaceKeepsRealNames(t *testing.T) {
	t.Parallel()
	keep := []string{
		// Multi-token names.
		"Maria Lopez", "Hans Müller", "Jean-Pierre Dubois", "Anna-Lena Weber",
		"Dr. Siewert", "John Doe", "O'Brien",
		// Single-token names (must survive — recall is sacred).
		"Mark", "Bill", "Will", "Madison", "Müller", "Lopez",
		// CJK name.
		"李伟",
		// Reversible First_Last(digits) username gold (ai4privacy).
		"Roma_Altenwerth", "Joe_Schuster53", "Edwin_Nitzsche",
		// Real orgs / places.
		"Deutsche Bank AG", "Acme Corp", "New York", "München", "São Paulo",
		"Baden-Württemberg",
		// Short all-letter tokens that are NOT machine shapes.
		"de", "IBM", "API", // bare 2-3 letter — no separator/digit → not locale/slug
	}
	for _, s := range keep {
		if isStructuralSurface(s) {
			t.Errorf("isStructuralSurface(%q) = true, want false (LEAK risk)", s)
		}
	}
}

// TestENAnomalyDropsStructuralSurfaces reproduces the #1 dashboard FP source:
// the EN anomaly bare path firing on capitalised-looking structural tokens in
// JSON / RPC blobs. After the guard, the structural surface emits nothing.
func TestENAnomalyDropsStructuralSurfaces(t *testing.T) {
	t.Parallel()
	r := NewENAnomalyRecognizer()
	// Each input is a structural token that the bare regex would otherwise
	// emit as a single-token PERSON candidate.
	for _, tok := range []string{
		"Conversation_Id", "Company_Id", "Gpt-4o", "Api_Key",
	} {
		text := "field " + tok + " value"
		res, err := r.Analyze(context.Background(), text, nil, "en")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", text, err)
		}
		for _, f := range res {
			if strings.EqualFold(text[f.Start:f.End], tok) {
				t.Errorf("EN anomaly emitted structural %q as PERSON (FP not suppressed)", tok)
			}
		}
	}
}

// TestENAnomalyKeepsRealNames is the recall-safety claim for the EN anomaly
// guard: real names still emit. The bare path scores 0.25 (below threshold)
// but the recognizer must still EMIT the candidate; the guard must not eat it.
func TestENAnomalyKeepsRealNames(t *testing.T) {
	t.Parallel()
	r := NewENAnomalyRecognizer()
	cases := []struct{ text, want string }{
		{"Dr. Sarah Williams examined the patient", "Sarah Williams"},
		{"Patient: Omar Hassan was admitted", "Omar Hassan"},
		{"John Smith was admitted", "John Smith"},
		{"Mark called the clinic", "Mark"},
	}
	for _, c := range cases {
		res, err := r.Analyze(context.Background(), c.text, nil, "en")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", c.text, err)
		}
		found := false
		for _, f := range res {
			if c.text[f.Start:f.End] == c.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EN anomaly DROPPED real name %q from %q (LEAK risk)", c.want, c.text)
		}
	}
}

// TestDEAnomalyKeepsRealNames confirms the DE anomaly guard never eats a real
// titled or multi-token German name.
func TestDEAnomalyKeepsRealNames(t *testing.T) {
	t.Parallel()
	r := NewDEAnomalyRecognizer()
	cases := []struct{ text, want string }{
		{"Dr. med. Hans Müller wurde aufgenommen", "Hans Müller"},
		{"Frau Maria Schneider stellte sich vor", "Maria Schneider"},
		{"Patient: Klaus Weber", "Klaus Weber"},
	}
	for _, c := range cases {
		res, err := r.Analyze(context.Background(), c.text, nil, "de")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", c.text, err)
		}
		found := false
		for _, f := range res {
			if c.text[f.Start:f.End] == c.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DE anomaly DROPPED real name %q from %q (LEAK risk)", c.want, c.text)
		}
	}
}

// TestENPersonGuardDropsStructuralKeepsNames asserts the wrapped ENPerson
// recognizer drops a structural surface but keeps the reversible
// First_Last(digits) username and honorific+name gold.
func TestENPersonGuardDropsStructuralKeepsNames(t *testing.T) {
	t.Parallel()
	r := NewENPersonRecognizer()

	// Real usernames / honorifics MUST survive.
	keep := []struct{ text, want string }{
		{"user Roma_Altenwerth logged in", "Roma_Altenwerth"},
		{"contact Joe_Schuster53 today", "Joe_Schuster53"},
		{"name Camilla10 in record", "Camilla10"},
	}
	for _, c := range keep {
		res, err := r.Analyze(context.Background(), c.text, nil, "en")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", c.text, err)
		}
		found := false
		for _, f := range res {
			if c.text[f.Start:f.End] == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("ENPerson DROPPED real surface %q from %q (LEAK risk)", c.want, c.text)
		}
	}

	// A structural token must NOT emit as PERSON. We feed surfaces shaped to
	// tempt the en_person patterns (Capitalised+digits, snake_case) but which
	// are structural; the guard must drop them.
	for _, tok := range []string{"Section4", "Iso27001"} {
		text := "name " + tok + " here"
		res, err := r.Analyze(context.Background(), text, nil, "en")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", text, err)
		}
		for _, f := range res {
			if text[f.Start:f.End] == tok && isStructuralSurface(tok) {
				t.Errorf("ENPerson emitted structural %q as PERSON", tok)
			}
		}
	}
}

// TestITDriverLicenseRejectsHexStructural reproduces the IT_DRIVER_LICENSE FP:
// the format-only (letter+A+7alnum+letter) regex collides with 10-char hex
// structural IDs. The pure-hex guard drops those while keeping real licences.
func TestITDriverLicenseRejectsHexStructural(t *testing.T) {
	t.Parallel()
	r := NewITDriverLicenseRecognizer()

	// Pure-hex 10-char surfaces that fit the format → must be dropped.
	drop := []string{
		"1a2b3c4d5e", // all hex, A in pos 2
		"facade1234", // looks like a hex slug
		"0a1b2c3d4e",
	}
	for _, s := range drop {
		text := "id " + s + " end"
		res, err := r.Analyze(context.Background(), text, nil, "it")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", text, err)
		}
		for _, f := range res {
			if text[f.Start:f.End] == s {
				t.Errorf("IT_DRIVER_LICENSE emitted structural hex %q (FP not suppressed)", s)
			}
		}
	}

	// Real-shaped licences with a non-hex char → must survive.
	keep := []string{
		"MA1234567Z", // trailing Z non-hex
		"GA9876543X",
		"TA0000000Y",
	}
	for _, s := range keep {
		text := "patente " + s
		res, err := r.Analyze(context.Background(), text, nil, "it")
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", text, err)
		}
		found := false
		for _, f := range res {
			if text[f.Start:f.End] == s {
				found = true
			}
		}
		if !found {
			t.Errorf("IT_DRIVER_LICENSE DROPPED real licence %q (LEAK risk)", s)
		}
	}
}
