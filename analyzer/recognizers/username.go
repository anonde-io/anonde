package recognizers

import (
	"context"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// Username / handle patterns: synthetic identifiers in the shape of
// firstname.lastname, surname+year, or random low-vowel stems with
// digit suffixes. ai4privacy_* gold buckets these as PERSON; the
// open-set NER models miss the long tail because the shapes don't
// match natural-language person-name distributions they were trained
// on.
//
// PATTERN PHILOSOPHY: username-shaped tokens are too weak to emit
// globally because they collide with protocol fields, model slugs, and
// code-ish identifiers in LLM/API traffic. Scored at 0.50 and gated by
// nearby account/social context so the analyzer still catches explicit
// username fields without redacting every stem+year token.
//
// FP guard rails to bound damage:
//
//   - Local context must mention username/account/handle/login/profile
//     or a social/Git hosting cue. Context-free bare forms are skipped.
//   - Stem ≥ 4 lowercase chars (so 1-3 letter common words like "the",
//     "and", "for" can't anchor a match).
//   - For the surname+year form, digit suffix must be 2-4 digits (so
//     "v1", "h3" don't trigger).
//   - For the dotted form, both halves must be ≥ 3 lowercase chars
//     (filters out "i.e.", "e.g.", file extensions like "img.png").
//   - Diacritics allowed in stems (Unicode `\p{Ll}` lowercase letter
//     class) so European usernames (gérardo.bötkös, schlöter01) match
//     the same way ASCII ones do.

var (
	// firstname.lastname[-suffix]; dotted handle shape. Both halves
	// ≥ 4 lowercase letters (Unicode letter class for diacritics).
	// The 4-char floor combined with a 5-char minimum first half is the
	// FP guard: "the.cat", "img.png", "doc.pdf", "max.min" all fail
	// because at least one half is < 4 chars. "bercem.luini" (6+5) and
	// "ada.lovelace42" (3+8+digits, wait, "ada" is 3 chars, needs
	// the optional digit suffix to lift it above the floor) survive
	// when the digit suffix is present.
	//
	// Optional 1-2 extra dot/hyphen segments cover "ada.lovelace-bot"
	// and "first.middle.last".  Optional 1-4 digit suffix covers
	// "ada.lovelace42". The (?:...) tail also raises the implicit
	// length, helping borderline cases.
	dottedHandleRE = regexp.MustCompile(
		`\b\d{0,3}\p{Ll}{4,30}[.\-]\p{Ll}{4,30}(?:[.\-]\p{Ll}{2,30}){0,2}\d{0,4}\b`,
	)

	// stem + digit suffix; surname-with-year shape like "kottmann1989",
	// "schlöter01", "jousse20". Stem ≥ 4 lowercase letters; suffix 2-4
	// digits. Random-token shapes like "xmrlcpvcqejqc8071" (stem 13
	// letters, 4 digits) match this form too.
	stemDigitRE = regexp.MustCompile(
		`\b\p{Ll}{4,30}\d{2,4}\b`,
	)

	usernameContextCues = []string{
		"username",
		"user name",
		"handle",
		"screen name",
		"login",
		"account",
		"profile",
		"social",
		"twitter",
		"x handle",
		"instagram",
		"mastodon",
		"bluesky",
		"github",
		"gitlab",
		"author",
		"created by",
		"posted by",
	}
)

// UsernameRecognizer detects synthetic / username-shaped PERSON tokens.
type UsernameRecognizer struct{}

// NewUsernameRecognizer constructs the recognizer.
func NewUsernameRecognizer() *UsernameRecognizer { return &UsernameRecognizer{} }

// Name returns the recognizer name (used in conflict resolution + logs).
func (r *UsernameRecognizer) Name() string { return "UsernameRecognizer" }

// SupportedEntities; emits PERSON (handle owner identity).
func (r *UsernameRecognizer) SupportedEntities() []string { return []string{"PERSON"} }

// SupportedLanguages; shape is language-agnostic.
func (r *UsernameRecognizer) SupportedLanguages() []string { return []string{"*"} }

// Analyze applies both username patterns. Scores are deliberately low
// (0.50) so a confident NER finding wins via the conflict resolver's
// NER-preferred rule for PERSON; this recognizer's job is to catch the
// tail GLiNER doesn't.
func (r *UsernameRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	for _, m := range dottedHandleRE.FindAllStringIndex(text, -1) {
		// Exclude the candidate span itself from the cue search: a dotted id
		// whose own segment is a cue (github.actions / profile.default /
		// account.settings) would otherwise self-corroborate as PERSON. Context
		// must come from OUTSIDE the span.
		if !hasLocalContextCueExcludingSelf(text, m[0], m[1], 56, 56, usernameContextCues) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.50,
			EntityType: "PERSON", RecognizerName: r.Name(),
		})
	}
	for _, m := range stemDigitRE.FindAllStringIndex(text, -1) {
		if !hasLocalContextCueExcludingSelf(text, m[0], m[1], 56, 56, usernameContextCues) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.50,
			EntityType: "PERSON", RecognizerName: r.Name(),
		})
	}
	return out, nil
}
