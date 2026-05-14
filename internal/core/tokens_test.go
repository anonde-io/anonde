package core

import (
	"strings"
	"testing"
)

// TestBuildTokenReplacer_PrefersLongerMatch verifies that overlapping
// tokens resolve via longest-first matching: a longer token can never
// be shadowed by a shorter one that is a prefix of it. This is what
// keeps reveal output correct when token IDs are extended (e.g.
// numeric suffixes that grow over time).
func TestBuildTokenReplacer_PrefersLongerMatch(t *testing.T) {
	t.Parallel()
	tokens := []string{
		"<EMAIL_ADDRESS_ACME_000001>",
		"<EMAIL_ADDRESS_ACME_000001_X>", // longer, must win on overlap
	}
	resolved := map[string]string{
		"<EMAIL_ADDRESS_ACME_000001>":   "alice@example.com",
		"<EMAIL_ADDRESS_ACME_000001_X>": "alice-extended@example.com",
	}
	replace, err := buildTokenReplacer(tokens, resolved)
	if err != nil {
		t.Fatalf("build replacer: %v", err)
	}
	in := "see <EMAIL_ADDRESS_ACME_000001_X> and <EMAIL_ADDRESS_ACME_000001>"
	out := replace(in)
	if !strings.Contains(out, "alice-extended@example.com") {
		t.Fatalf("expected longer token resolved, got %q", out)
	}
	if !strings.Contains(out, "alice@example.com") {
		t.Fatalf("expected shorter token resolved, got %q", out)
	}
}
