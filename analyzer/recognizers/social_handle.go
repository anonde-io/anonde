package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// Social-media usernames. Twitter/X / Mastodon / Bluesky handles are
// `@<username>`. The explicit `@`-prefix form emits PERSON because that's
// how account owners are scored in the canonical label map. Hashtags emit
// ORGANIZATION only in social-media context.
//
// The two-pattern split:
//   - explicit `@handle`: 0.85, anchored on `@`, can fire anywhere.
//   - `#hashtag`:         0.78, requires surrounding social-text cues to
//                         escape FP land on markdown / prompt text.

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

	hashtagContextCues = []string{
		"hashtag",
		"tweet",
		"retweet",
		"twitter",
		"x post",
		"instagram",
		"mastodon",
		"bluesky",
		"social",
		"tagged",
		"follow",
		"post",
		"posted",
		"trending",
	}
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

// Analyze emits explicit `@handle` matches globally. Hashtags only emit
// as ORGANIZATION when nearby text indicates social-media context; raw
// markdown headings and arbitrary #tokens are otherwise too noisy. Bare
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
		// Exclude the hashtag span itself from the cue search: otherwise a
		// hashtag whose own text IS a cue (#social / #tweet / #trending) would
		// authorize itself as ORGANIZATION with no external social context.
		if !hasLocalContextCueExcludingSelf(text, m[2], m[3], 56, 56, hashtagContextCues) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start: m[2], End: m[3], Score: 0.78,
			EntityType: "ORGANIZATION", RecognizerName: r.Name(),
		})
	}
	return out, nil
}
