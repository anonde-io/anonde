// Post-validator for decoded NER spans, in two layers:
//
//   - MoneyGuard (default ON for NER): drops ID/POSTAL_CODE spans shaped like
//     a currency amount ("8.750 EUR"). An amount is never PII whatever the
//     label set; the matrix proves zero gold-span overlap. This is what fixes
//     the finance/legal/clinical money→ID over-redaction by default.
//   - Structural shape filter (Enabled, OPT-IN via GLINER_SPAN_FILTER): drops
//     fuzzy-type (PERSON/ORG/LOCATION/...) spans whose surface is a UUID /
//     locale / semver / model-slug / hex / base64 / SCREAMING_SNAKE / dotted
//     path, plus a stoplist. A precision tool that trades recall: on PII-dense
//     synthetic corpora (ai4privacy, synth_logs) these shapes overlap gold
//     usernames / ZIPs / coordinates, so it stays opt-in — see the bench
//     matrix in the span-filter scope-back.
//
// LegalNoise layers statute/exhibit ID rejection for GLINER_LABEL_SET=legal.
// Structured types (EMAIL/IBAN/CARD/...) are never touched by the shape rules.
//
// Not build-tagged on purpose — pure data, so it is unit-tested and benched in
// the default no-CGO build and reused by every GLiNER variant under -tags ner.

package recognizers

import (
	"regexp"
	"strings"
)

// spanFilterFuzzyTypes is the set of name-like types the filter may reject on.
// Structured types are deliberately absent: a structural surface labelled as
// one of those is likely a real identifier we want to keep.
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

	// Monetary amount: "8.750 EUR", "2.300,00 EUR", "€42". A currency
	// marker is REQUIRED so bare digit runs (real account / case-number
	// fragments) still match no shape and stay redacted. Universal — an
	// amount is never PII whatever the label set; applied to ID/POSTAL_CODE.
	reMoney = regexp.MustCompile(`^\s*(?:(?:EUR|USD|GBP|CHF|€|\$|£)\s*\d[\d.,]*|\d[\d.,]*\s*(?:EUR|USD|GBP|CHF|€|\$|£))\s*$`)

	// Statute / code reference: "§ 29 ZPO", "§§ 330 ff. ZPO". A section sign
	// anywhere, or a bare German-code abbreviation. Legal-only.
	reLegalStatute  = regexp.MustCompile(`§`)
	reLegalCodeAbbr = regexp.MustCompile(`\b(?:ZPO|BGB|StGB|StPO|HGB|GG|InsO|FamFG|GVG|AktG|GmbHG|EGBGB|RVG|GKG|VwGO|SGB|AO)\b`)

	// Exhibit / attachment label: "K1", "Anlage K3". Legal-only.
	reLegalExhibit = regexp.MustCompile(`^(?:Anlage\s+)?[A-Z]\d{1,3}$`)
)

// SpanFilterConfig configures the structural-shape post-filter. The zero
// value is a no-op (Enabled=false). Wire it onto GLiNERConfig.SpanFilter;
// the span/flat/pool/ensemble recognizers all consult the same field.
type SpanFilterConfig struct {
	// Enabled turns on the OPT-IN structural shape filter + stoplist for
	// fuzzy types. Independent of MoneyGuard.
	Enabled bool

	// MoneyGuard turns on the universal currency-amount rejection for
	// ID/POSTAL_CODE. Default ON for the NER path; applies even when the
	// shape filter (Enabled) is off.
	MoneyGuard bool

	// Stoplist is a set of lower-cased surfaces dropped for the fuzzy
	// types regardless of shape. Seeded via NewSpanFilter/StrictSpanFilter;
	// keys MUST be lower-case (lookups lower-case the surface first).
	// Consulted only when Enabled.
	Stoplist map[string]bool

	// LegalNoise additionally rejects ID/POSTAL_CODE spans shaped like a
	// statute ref ("§ 29 ZPO") or exhibit label ("K1", "Anlage K3").
	// Legal-only — set by LegalSpanFilter; consulted only when Enabled.
	LegalNoise bool
}

// Active reports whether the config rejects anything (either layer on).
func (f SpanFilterConfig) Active() bool { return f.Enabled || f.MoneyGuard }

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

// MoneyGuardFilter is the universal default-on NER profile: the currency
// guard on ID/POSTAL_CODE, and nothing else. Leak-safe across the whole bench
// matrix (zero gold-span overlap); the opt-in shape rules are NOT included.
func MoneyGuardFilter() SpanFilterConfig {
	return SpanFilterConfig{MoneyGuard: true}
}

// universalNonNameSurfaces is an always-on denylist of WHOLE-surface strings
// GLiNER recurrently mislabels as a fuzzy PII type (PERSON / ORGANIZATION /
// LOCATION / NRP / PROFESSION / AGE) but which are never a named individual,
// org, or place. It lives in the MoneyGuard tier (always-on) rather than the
// opt-in Enabled stoplist because the default NER deploy runs MoneyGuardFilter()
// with Enabled=false, so an Enabled-gated stoplist would be a no-op in
// production. Every entry is verified leak-safe on the gold corpora (drops only
// pure FPs, zero gold overlap) and is matched only against the full trimmed
// surface, so a multi-token name like "Patient Müller" is untouched.
//
// Discipline: no first name, surname, real place, or job-title fragment that
// appears in any gold span (notably absent: administrator/employee/manager/
// engineer, and clinical patient-role nouns — see the inline notes). Extend
// only with a surface re-verified leak-safe on the gold matrix.
var universalNonNameSurfaces = buildNonNameSurfaceSet([]string{
	// Pronouns (EN + DE).
	"i", "you", "we", "he", "she", "it", "they",
	"sie", "wir", "er", "es",
	// Polite / imperative / discourse leads.
	"please", "kindly", "hello", "hi", "thanks", "thank you", "regards",
	"dear", "use", "check", "contact", "note", "attention", "all",
	"let's", "we've",
	// Generic role/relation nouns (EN). "administrator"/"employee" are excluded
	// (they appear inside gold org/title spans on ai4privacy); so are clinical
	// patient-role nouns (dropping them perturbs the adjacent AGE-span merge and
	// trips the openmed leak floor).
	"child", "children", "student", "students",
	"parent", "parents", "client", "clients", "customer", "customers",
	"user", "users", "member", "members", "caller", "applicant",
	"guest", "tenant", "subscriber", "vendor",
	// DE relation common nouns (clinical patient-role nouns excluded above).
	"kollege", "kollegin", "frau kollegin", "herr kollege",
	"großmutter", "grossmutter", "mutter", "vater", "eltern",
	"mädchen", "freundin", "freund",
	// Demographic tokens.
	"male", "female",
	// Browser / user-agent tokens.
	"mozilla", "gecko", "khtml", "trident", "applewebkit", "webkit",
	"firefox", "opera", "presto", "macintosh", "windows nt",
	"intel mac os", "linux", "x11", "chrome", "safari", "edge",
	// Synthetic finance account-type phrases.
	"savings account", "investment account", "checking account",
	"credit card account", "auto loan account", "personal loan account",
	"home loan account", "money market account", "brokerage account",
	// Card-brand slugs (lower-case underscore forms GLiNER tags as ORG).
	"diners_club", "american_express", "discover", "maestro",
	"mastercard", "visa", "jcb",
	// Misc non-PII tech tokens.
	"ethereum", "bitcoin", "iban",
})

func buildNonNameSurfaceSet(terms []string) map[string]bool {
	m := make(map[string]bool, len(terms))
	for _, t := range terms {
		m[strings.ToLower(t)] = true
	}
	return m
}

// NewSpanFilter returns the OPT-IN shape filter: stoplist + structural shape
// rules + the money guard, seeded with the default stoplist plus any extra
// lower-cased terms. Prefer this over constructing the struct by hand.
func NewSpanFilter(extra ...string) SpanFilterConfig {
	sl := defaultSpanFilterStoplist()
	for _, t := range extra {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			sl[t] = true
		}
	}
	return SpanFilterConfig{Enabled: true, MoneyGuard: true, Stoplist: sl}
}

// StrictSpanFilter is the same span-shape filter as the default; STRICT's
// extra is raised per-class thresholds (set by the caller), not extra shape
// rules. Kept as a stable name so the two can diverge without churning call
// sites.
func StrictSpanFilter(extra ...string) SpanFilterConfig {
	return NewSpanFilter(extra...)
}

// legalRoleStoplist is the German legal party-role / boilerplate denylist
// (Kläger, Beklagte, Partei, ...) GLiNER's legal label set mislabels as
// PERSON / ORGANIZATION. Role titles, not named individuals. Lower-cased.
func legalRoleStoplist() []string {
	return []string{
		// Plaintiff / defendant (+ gender/case inflections).
		"kläger", "klägers", "klägerin", "klägern",
		"beklagte", "beklagter", "beklagten", "beklagtin",
		// Applicant / respondent (Beschluss / einstweilige Verfügung).
		"antragsteller", "antragstellerin", "antragsgegner", "antragsgegnerin",
		// Counsel / representative.
		"klägervertreter", "beklagtenvertreter",
		"bevollmächtigte", "bevollmächtigter", "bevollmächtigten",
		"unseres mandanten", "mandant", "mandantin",
		"mandantschaft", "mandantschaften",
		"unterzeichner", "unterzeichners",
		"vollmachtgeber",
		// Court structure / party-collective / boilerplate nouns.
		"partei", "parteien", "klagepartei", "beide parteien",
		"zivilkammer", "kammer", "kammerbezirk", "geschäftsstelle",
		"damen und herren", "säumnis", "streitwert", "klage",
		// Contract / object common nouns GLiNER tags as ORGANIZATION.
		"kaufvertrag", "maschinenteilen",
	}
}

// LegalSpanFilter layers the legal profile on the universal filter: the
// legal-role stoplist + the LegalNoise statute/exhibit ID/POSTAL_CODE
// suppressor. Wired when GLINER_LABEL_SET=legal. Extra terms lower-cased.
func LegalSpanFilter(extra ...string) SpanFilterConfig {
	sf := NewSpanFilter(append(legalRoleStoplist(), extra...)...)
	sf.LegalNoise = true
	return sf
}

// Reject is the exported form of rejectSpanSurface for out-of-package
// callers (the bench/probes/span_shape probe). Production recognizers call
// the unexported method directly.
func (f SpanFilterConfig) Reject(entityType, surface string) bool {
	return f.rejectSpanSurface(entityType, surface)
}

// rejectSpanSurface reports whether a decoded (type, surface) span should be
// dropped. Pure function of (config, type, surface) — the single decision
// point every recognizer and the tests share.
func (f SpanFilterConfig) rejectSpanSurface(entityType, surface string) bool {
	s := strings.TrimSpace(surface)

	// ID/POSTAL_CODE noise, checked before the fuzzy-type gate (these are
	// structured types the shape rules never touch). Money guard is universal
	// (an amount is never PII, whatever the label set) and gated on MoneyGuard
	// so it runs even with the shape filter off; statute/exhibit refs are
	// legal-only and require Enabled+LegalNoise. A currency marker is required
	// for money, so bare digit runs (real account / case-number fragments)
	// match nothing and stay redacted.
	if (entityType == "ID" || entityType == "POSTAL_CODE") && s != "" {
		if f.MoneyGuard && reMoney.MatchString(s) {
			return true
		}
		if f.Enabled && f.LegalNoise && (reLegalStatute.MatchString(s) ||
			reLegalCodeAbbr.MatchString(s) ||
			reLegalExhibit.MatchString(s)) {
			return true
		}
	}

	// Universal non-name surface guard (MoneyGuard tier, always-on for the
	// default NER profile). Drops a fuzzy-type span whose whole trimmed surface
	// is a known never-a-name token. Matched on the full surface only, so it
	// never trims inside a real multi-token name.
	if f.MoneyGuard && spanFilterFuzzyTypes[entityType] && s != "" {
		if universalNonNameSurfaces[strings.ToLower(s)] {
			return true
		}
	}

	// The shape rules + stoplist below are the opt-in layer.
	if !f.Enabled {
		return false
	}

	if !spanFilterFuzzyTypes[entityType] {
		return false
	}

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
