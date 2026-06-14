// Structural-shape post-validator for decoded NER spans. GLiNER is
// open-set and will tag a model slug ("gpt-4o"), a UUID, a locale code,
// or a semver string as PERSON/ORGANIZATION/LOCATION because those
// surfaces are lexically name-shaped; on request-metadata-heavy traffic
// that floods false positives and corrupts requests. After a span is
// decoded, if its surface matches a known structural shape AND its type is
// a fuzzy/name-like type, reject it. Structured types (EMAIL/IBAN/CARD/...)
// are never touched: a UUID the model called an "id number" is plausibly a
// real ID we want. Every reject rule is conservative, so enabling the
// filter only raises precision and leaves real-PII recall unchanged.
//
// Not build-tagged: pure-data, so it is defined, unit-tested, and benched
// in the default (no-CGO) build and reused by every GLiNER variant
// (span / flat / pool / ensemble) under -tags hugot.

package recognizers

import (
	"regexp"
	"strings"
)

// spanFilterFuzzyTypes is the set of canonical entity types the shape
// filter may reject on — the open-set, name-like types where GLiNER
// over-fires on structural surfaces. Structured types are deliberately
// absent: a structural surface labelled as one of those is likely a real
// identifier we want to keep.
var spanFilterFuzzyTypes = map[string]bool{
	"PERSON":       true,
	"ORGANIZATION": true,
	"LOCATION":     true,
	"NRP":          true,
	"PROFESSION":   true,
	"AGE":          true,
}

// Structural-shape recognisers. Anchored (^...$) so they classify the
// WHOLE surface, never a substring: "Maria UUID" must not be rejected just
// because it contains a hex run.
var (
	// Canonical 8-4-4-4-12 hex UUID (any case), optionally braced.
	reUUID = regexp.MustCompile(`^\{?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\}?$`)

	// BCP-47-ish locale tag (en-US, de_DE, zh-Hans-CN). Requires a
	// separator so bare "de" — which could be a name — is NOT matched.
	reLocale = regexp.MustCompile(`^[a-z]{2,3}([-_][A-Za-z]{2,4}){1,3}$`)

	// Semver / version: v1.2.3, 1.81.1, 2.0.0-rc1.
	reVersion = regexp.MustCompile(`^[vV]?\d+(\.\d+){1,3}([-+][0-9A-Za-z.]+)?$`)

	// Model slug: gpt-4o / llama-2 / text-davinci-003. Lowercase-leading
	// and case-sensitive so it does not eat "Côte-d'Or".
	reModelSlug = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*-?\d+[a-z0-9.]*$`)

	// Pure hex run of >=16 chars (SHAs, hashes, keys). Length-gated since
	// shorter hex can be a real token.
	reHexBlob = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)

	// base64/base64url blob >=24 chars. The has-digit guard avoids
	// rejecting a long all-letter name.
	reBase64Alphabet = regexp.MustCompile(`^[A-Za-z0-9+/_=-]{24,}$`)
	reBase64HasDigit = regexp.MustCompile(`[0-9+/_=]`)

	// SCREAMING_SNAKE identifiers (HTTP_X_FORWARDED, API_KEY). Requires an
	// underscore so short acronym names (e.g. "IBM") are spared.
	reAllCapsUnderscore = regexp.MustCompile(`^[A-Z0-9]+(_[A-Z0-9]+)+$`)

	// Pure digits/punctuation, no letters (timestamps, ratios). AGE is
	// numeric, so the caller exempts it from this rule.
	reDigitsPunct = regexp.MustCompile(`^[\d\p{P}\p{S}\s]+$`)

	// Dotted identifiers / hostnames / package paths. >=2 dots so "Dr.
	// Smith" and single-initial names are not caught.
	reDottedPath = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+){2,}$`)
)

// SpanFilterConfig configures the structural-shape post-filter. The zero
// value is a no-op (Enabled=false). Wire it onto GLiNERConfig.SpanFilter;
// the span/flat/pool/ensemble recognizers all consult the same field.
type SpanFilterConfig struct {
	Enabled bool

	// Stoplist is a set of lower-cased surfaces dropped for the fuzzy
	// types regardless of shape. Seeded via NewSpanFilter/StrictSpanFilter;
	// keys MUST be lower-case (lookups lower-case the surface first).
	Stoplist map[string]bool
}

// defaultSpanFilterStoplist is the seed denylist of recurring non-PII
// surfaces GLiNER mislabels as PERSON/ORG/LOCATION on LLM-proxy and log
// traffic. Intentionally small and high-signal; extend via GLINER_STOPLIST.
func defaultSpanFilterStoplist() map[string]bool {
	terms := []string{
		"gpt", "gpt-4", "gpt-4o", "gpt-4o-mini", "gpt-3.5", "gpt-3.5-turbo",
		"chatgpt", "davinci", "text-davinci-003", "o1", "o1-mini", "o3",
		"claude", "claude-3", "claude-3.5", "sonnet", "opus", "haiku",
		"gemini", "gemini-pro", "palm", "bard",
		"llama", "llama-2", "llama-3", "llama3", "mistral", "mixtral",
		"falcon", "phi", "qwen", "deepseek", "grok", "command-r",
		"openai", "anthropic", "cohere", "huggingface",
		"json", "yaml", "xml", "http", "https", "tcp", "udp", "url", "uri",
		"api", "sdk", "cli", "uuid", "guid", "token", "bearer", "oauth",
		"null", "true", "false", "none", "nan", "undefined",
		"get", "post", "put", "patch", "delete", "head", "options",
		"utf-8", "ascii", "base64", "sha256", "md5", "regex",
	}
	m := make(map[string]bool, len(terms))
	for _, t := range terms {
		m[strings.ToLower(t)] = true
	}
	return m
}

// NewSpanFilter returns an enabled SpanFilterConfig seeded with the
// default stoplist plus any extra lower-cased terms. Prefer this over
// constructing the struct by hand so the default denylist is present.
func NewSpanFilter(extra ...string) SpanFilterConfig {
	sl := defaultSpanFilterStoplist()
	for _, t := range extra {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			sl[t] = true
		}
	}
	return SpanFilterConfig{Enabled: true, Stoplist: sl}
}

// StrictSpanFilter is the named precision profile the STRICT deploy and
// docs refer to: shape rejection plus the default stoplist. Identical to
// NewSpanFilter() today; exists as a stable name so the two can diverge
// later without churning call sites.
func StrictSpanFilter(extra ...string) SpanFilterConfig {
	return NewSpanFilter(extra...)
}

// Reject is the exported form of rejectSpanSurface for out-of-package
// callers (the bench/probes/span_shape probe). Production recognizers call
// the unexported method directly.
func (f SpanFilterConfig) Reject(entityType, surface string) bool {
	return f.rejectSpanSurface(entityType, surface)
}

// rejectSpanSurface reports whether a decoded span with the given canonical
// entity type and surface text should be rejected. Returns false (keep)
// when disabled, when the type is not fuzzy, or when the surface is
// plausibly real PII; true (drop) only on a structurally incompatible
// surface. Pure function of (config, type, surface); the single decision
// point every recognizer and the tests call.
func (f SpanFilterConfig) rejectSpanSurface(entityType, surface string) bool {
	if !f.Enabled {
		return false
	}
	if !spanFilterFuzzyTypes[entityType] {
		return false
	}

	s := strings.TrimSpace(surface)
	if s == "" {
		return true
	}

	if len(f.Stoplist) > 0 && f.Stoplist[strings.ToLower(s)] {
		return true
	}

	// AGE is numeric ("42", "42 years"), so the digit/punct rule must NOT
	// apply to it; every other shape rule is still safe for AGE.
	isAge := entityType == "AGE"

	switch {
	case reUUID.MatchString(s):
		return true
	case reLocale.MatchString(s):
		return true
	case reVersion.MatchString(s):
		return true
	case reModelSlug.MatchString(s):
		return true
	case reHexBlob.MatchString(s):
		return true
	case reAllCapsUnderscore.MatchString(s):
		return true
	case reDottedPath.MatchString(s):
		return true
	case reBase64Alphabet.MatchString(s) && reBase64HasDigit.MatchString(s):
		return true
	case !isAge && reDigitsPunct.MatchString(s):
		return true
	}
	return false
}
