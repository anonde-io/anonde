package recognizers

// SecretRecognizer detects machine credentials / secrets (API keys, tokens,
// private keys) as a high-precision, leak-safe recognizer.
// ReDoS safety
// ------------
// Every pattern here is a Go `regexp` (RE2) pattern. RE2 matches in time
// linear in the input length and supports no backreferences / lookaround, so
// it is ReDoS-safe *by construction* — there is no catastrophic-backtracking
// surface to guard against. Where a gitleaks rule relies on lookaround, it is
// ported as a broad RE2 match plus a post-match validation function
// (isPlausibleSecretValue / the shape guards) rather than a backtracking regex.
//
// Precision philosophy
// --------------------
// A secret detector that redacts random hex/base64 is worse than useless: it
// trains users to ignore it. So this recognizer is deliberately conservative.
//   - Provider rules fire only on tokens with a distinctive, unique prefix
//     (ghp_, AKIA, sk_live_, AIza, …) — these are near-zero false positive.
//   - The generic / entropy layer fires ONLY on a value that is directly
//     assigned to a secret keyword (`api_key = "..."`, `token=...`). The
//     surrounding keyword is the evidence; a free-floating high-entropy blob
//     (a git SHA, a UUID, a base64 data field with no secret context) is NOT
//     flagged. This is the load-bearing precision decision: we would rather
//     miss a context-less secret than redact every hash in a document.

import (
	"context"
	"math"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

// secretRule is one provider pattern plus an optional post-match validator.
type secretRule struct {
	id    string
	re    *regexp.Regexp
	score float64
	// valid, if non-nil, must return true for the matched substring or the
	// match is discarded (ports gitleaks lookaround as RE2 + validation).
	valid func(string) bool
}

var (
	// providerRules: distinctive-prefix credentials. Cheap and near-zero FP,
	// so they run on EVERY input (no keyword pre-filter needed) — the gitleaks
	// pre-filter trick reserves the pre-filter for the expensive generic layer.
	providerRules = []secretRule{
		// AWS access key id.
		{id: "aws-access-key", score: 1.0,
			re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
		// GitHub PAT / OAuth / app / refresh tokens (ghp_/gho_/ghu_/ghs_/ghr_).
		{id: "github-pat", score: 1.0,
			re: regexp.MustCompile(`\bgh[opsur]_[0-9A-Za-z]{36}\b`)},
		// GitLab personal access token.
		{id: "gitlab-pat", score: 1.0,
			re: regexp.MustCompile(`\bglpat-[0-9A-Za-z_-]{20}\b`)},
		// Stripe secret / restricted live|test keys. Charset broadened to
		// base64url to cover synthetic emissions; the sk_/rk_ + live/test
		// prefix keeps it specific.
		{id: "stripe-key", score: 1.0,
			re: regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[0-9A-Za-z_-]{16,}\b`)},
		// Slack token (bot/user/app/refresh/legacy).
		{id: "slack-token", score: 1.0,
			re: regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`)},
		// Slack incoming-webhook URL.
		{id: "slack-webhook", score: 1.0,
			re: regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9+/]{40,}`)},
		// Google API key.
		{id: "google-api-key", score: 1.0,
			re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
		// SendGrid API key.
		{id: "sendgrid-key", score: 1.0,
			re: regexp.MustCompile(`\bSG\.[0-9A-Za-z_-]{22}\.[0-9A-Za-z_-]{43}\b`)},
		// Twilio API key SID (SK + 32 hex). Medium specificity → 0.9.
		{id: "twilio-key", score: 0.9,
			re: regexp.MustCompile(`\bSK[0-9a-f]{32}\b`)},
		// npm access token.
		{id: "npm-token", score: 1.0,
			re: regexp.MustCompile(`\bnpm_[0-9A-Za-z]{36}\b`)},
		// PyPI upload token.
		{id: "pypi-token", score: 1.0,
			re: regexp.MustCompile(`\bpypi-[0-9A-Za-z_-]{16,}\b`)},
		// Anthropic API key (checked before the generic sk- OpenAI rule).
		{id: "anthropic-key", score: 1.0,
			re: regexp.MustCompile(`\bsk-ant-[0-9A-Za-z_-]{20,}\b`)},
		// OpenAI-style secret key (sk- / sk-proj-). Dash distinguishes it from
		// Stripe's sk_ underscore form.
		{id: "openai-key", score: 0.95,
			re: regexp.MustCompile(`\bsk-(?:proj-)?[0-9A-Za-z]{20,}\b`)},
		// Real JWT (registered-claim header begins `eyJ`). The generic layer
		// catches keyword-anchored opaque JWTs that lack this header.
		{id: "jwt", score: 1.0,
			re: regexp.MustCompile(`\beyJ[0-9A-Za-z_-]{8,}\.eyJ[0-9A-Za-z_-]{8,}\.[0-9A-Za-z_-]{8,}\b`)},
		// PEM private-key block (RSA/EC/DSA/OPENSSH/PGP or bare). `(?s)` lets
		// `.` span newlines; the END is optional so a truncated header still
		// flags. RE2 → linear, no backtracking blowup on the lazy `.*?`.
		{id: "private-key", score: 1.0,
			re: regexp.MustCompile(`(?s)-----BEGIN[ A-Z0-9]*PRIVATE KEY-----(?:.*?-----END[ A-Z0-9]*PRIVATE KEY-----)?`)},
	}

	// keywordPrefilter is the cheap gate for the expensive generic layer: if
	// the text contains no secret-ish keyword at all, skip the assignment scan
	// entirely (gitleaks' pre-filter optimisation). It is a strict superset of
	// the keyword alternation in assignmentRE (keyword presence, no delimiter),
	// so it can never wrongly skip a line the assignment scan would have hit.
	keywordPrefilter = regexp.MustCompile(`(?i)(client[ _-]?secret|access[ _-]?key|api[ _-]?key|apikey|old_key|new_key|private[ _-]?key|secret|token|passw|pwd|bearer|jwt|session|\bsess\b|\bkey\b|credential)`)

	// assignmentRE captures a value directly bound to a secret keyword via
	// `:` or `=` (optionally `Bearer:` connective, optionally quoted). The
	// keyword group is NON-capturing; capture group 1 is the value, which is
	// what gets emitted — never the keyword — so spans align with gold
	// secret-token spans, not `key=...` field names.
	//
	// The keyword is the evidence; the value still has to pass
	// isPlausibleSecretValue before it is accepted.
	assignmentRE = regexp.MustCompile(`(?i)\b(?:client[ _-]?secret|access[ _-]?key|api[ _-]?key|apikey|old_key|new_key|private[ _-]?key|secret|token|passw(?:or)?d|pwd|bearer|jwt|session|sess|key|credential)\b\s*[:=]\s*(?:Bearer[:\s]+)?["']?([A-Za-z0-9_-][A-Za-z0-9._/+=~-]{15,})`)

	// Shape guards — exact non-secret shapes excluded from the entropy layer.
	uuidRE   = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	semverRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+([.\-+][0-9A-Za-z.-]*)?$`)
	localeRE = regexp.MustCompile(`^[a-z]{2,3}([-_][A-Za-z]{2,4}){0,2}$`)
)

// SecretRecognizer implements analyzer.EntityRecognizer for the SECRET type.
type SecretRecognizer struct{}

// NewSecretRecognizer constructs the secret/credential recognizer.
func NewSecretRecognizer() *SecretRecognizer { return &SecretRecognizer{} }

func (s *SecretRecognizer) Name() string                 { return "SecretRecognizer" }
func (s *SecretRecognizer) SupportedEntities() []string  { return []string{"SECRET"} }
func (s *SecretRecognizer) SupportedLanguages() []string { return []string{"*"} }

func (s *SecretRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult

	// Layer A — provider rules (always-on, distinctive prefix, cheap).
	for _, rule := range providerRules {
		for _, m := range rule.re.FindAllStringIndex(text, -1) {
			if rule.valid != nil && !rule.valid(text[m[0]:m[1]]) {
				continue
			}
			results = append(results, analyzer.RecognizerResult{
				Start: m[0], End: m[1], Score: rule.score,
				EntityType: "SECRET", RecognizerName: "SecretRecognizer",
			})
		}
	}

	// Layer B — generic keyword-anchored entropy layer. Gated on the cheap
	// keyword pre-filter: no secret-ish keyword anywhere → nothing to do.
	if keywordPrefilter.MatchString(text) {
		for _, m := range assignmentRE.FindAllStringSubmatchIndex(text, -1) {
			// m[2]:m[3] is capture group 1 (the value).
			vs, ve := m[2], m[3]
			if vs < 0 {
				continue
			}
			val := text[vs:ve]
			if !isPlausibleSecretValue(val) {
				continue
			}
			results = append(results, analyzer.RecognizerResult{
				Start: vs, End: ve, Score: 0.95,
				EntityType: "SECRET", RecognizerName: "SecretRecognizer",
			})
		}
	}

	return results, nil
}

// isPlausibleSecretValue gates a keyword-anchored candidate value. Because the
// preceding keyword is decisive evidence of secret intent, this is deliberately
// permissive about *shape* — a 40-char hex assigned to `api_key=` IS a secret
// even though a bare 40-char hex elsewhere is (correctly) treated as a git SHA.
// It rejects only provably-non-secret exact shapes and low-information runs.
func isPlausibleSecretValue(v string) bool {
	if len(v) < 16 {
		return false
	}
	// Exact non-secret shapes: a session/token UUID is not a high-value
	// credential, and version/locale strings are never secrets.
	if uuidRE.MatchString(v) || semverRE.MatchString(v) || localeRE.MatchString(v) {
		return false
	}
	if isMonotoneRun(v) {
		return false
	}
	// Entropy floor: random secrets sit well above natural-language /
	// repetitive strings. Hex (16-symbol alphabet) tops out near 4.0; this
	// 3.0 floor clears real hex/base64 secrets while rejecting low-information
	// runs that slipped past the shape guards.
	return shannonEntropy(v) >= 3.0
}

// isMonotoneRun reports whether every byte in s is identical (e.g. "aaaa…").
func isMonotoneRun(s string) bool {
	if len(s) == 0 {
		return true
	}
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}

// shannonEntropy computes the per-character Shannon entropy (bits) of s over
// its raw bytes.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	var freq [256]float64
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}
