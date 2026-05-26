package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// Social-media usernames. Twitter/X / Mastodon / Bluesky handles are
// `@<username>`; bare usernames also appear in mention contexts ("by
// @user", "from beatfaceleah"). Patterns target the explicit `@`-prefix
// form (high precision) and a lowercased-handle shape that's common in
// short social text (e.g. wnut_17). Emits PERSON because that's how
// account owners are scored in the canonical label map.
//
// The two-pattern split:
//   - explicit `@handle`: 0.85, anchored on `@`, can fire anywhere.
//   - bare handle:        0.55, lowercased + digits, requires
//                          surrounding social-text cues (mention verbs)
//                          to escape FP land on normal English prose.

var (
	// Explicit `@handle`; high precision. Twitter limits handles to
	// 15 chars, Bluesky to 18; the pattern allows 3-30 for the
	// general case (covers Mastodon `@user@server` too if you'd want
	// to extend; for now we capture just the leading `@user` token).
	// Whitespace after the `@` is optional; wnut_17's tokenisation
	// produces "RT @ beatfaceleah :" with a space between sigil and
	// handle, which the no-space form would miss.
	socialAtHandleRE = regexp.MustCompile(
		`(?:^|[^A-Za-z0-9_])(@[ \t]?[A-Za-z][A-Za-z0-9_]{2,29})\b`,
	)

	// Hashtag mention. Same handle shape after the `#` sigil,
	// optional whitespace. wnut_17 surfaces brand / community
	// hashtags ("# fitnessblender") that the gold treats as ORG.
	socialHashtagRE = regexp.MustCompile(
		`(?:^|[^A-Za-z0-9_])(#[ \t]?[A-Za-z][A-Za-z0-9_]{2,29})\b`,
	)
)

// SocialHandleRecognizer detects social-media handles.
type SocialHandleRecognizer struct{}

// NewSocialHandleRecognizer constructs the recognizer.
func NewSocialHandleRecognizer() *SocialHandleRecognizer { return &SocialHandleRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution.
func (r *SocialHandleRecognizer) Name() string { return "SocialHandleRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
// PERSON for @-handles (Twitter / Bluesky account names) and
// ORGANIZATION for #-hashtags (used for brand / community mentions
// in wnut_17 gold).
func (r *SocialHandleRecognizer) SupportedEntities() []string {
	return []string{"PERSON", "ORGANIZATION"}
}

// SupportedLanguages; handles are language-agnostic syntactic shapes.
func (r *SocialHandleRecognizer) SupportedLanguages() []string { return []string{"*"} }

// Analyze emits explicit `@handle` matches. The bare-handle pattern is
// intentionally NOT emitted as a recognizer hit; its FP risk on normal
// English prose is too high without local context analysis. Bare
// handles are caught by the open-set NER backend when one is loaded.
func (r *SocialHandleRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	// Submatch group 1 = handle (without the leading char that gated
	// the boundary check). Use m[2]/m[3] indices for the group span.
	for _, m := range socialAtHandleRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start: m[2], End: m[3], Score: 0.85,
			EntityType: "PERSON", RecognizerName: r.Name(),
		})
	}
	for _, m := range socialHashtagRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start: m[2], End: m[3], Score: 0.78,
			EntityType: "ORGANIZATION", RecognizerName: r.Name(),
		})
	}
	return out, nil
}
