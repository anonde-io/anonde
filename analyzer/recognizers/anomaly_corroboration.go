package recognizers

import "strings"

// Corroboration gate for the heuristic anomaly-PERSON recognizers
// (en_anomaly / de_anomaly / en_person). It suppresses a SINGLE-token
// capitalised PERSON candidate ONLY when it has no local name-corroborating
// signal AND sits on an FP-indicating STRUCTURAL surface (a JSON-key / label
// position, an English contraction, or a user-agent / version path). This is
// the generalisable, language-agnostic complement to isStructuralSurface:
// isStructuralSurface classifies the WHOLE token shape (UUID / hex / snake_case
// …); this classifies the token's IMMEDIATE PUNCTUATION CONTEXT.
//
// Leak-safety by construction — three disciplines:
//
//  1. SINGLE-token only. A multi-token capitalised run ("John Smith",
//     "New York") is never touched, so real multi-word names are preserved.
//     Only a lone capitalised token can be gated.
//
//  2. FP signal must be POSITIVE and STRUCTURAL, never mere absence of a cue.
//     The gate fires only when the token is written as a machine/label
//     surface — a key/label before ':' or '=', an English contraction
//     ("We've"), or a "/<digit>" version path ("Mozilla/5.0"). A bare name in
//     prose ("patient Smith was admitted", "Dear Omer,") carries NONE of these
//     signals, so it is never gated. This is why the gate is leak-byte-identical
//     on the gold corpora: gold PERSON spans are prose names, structurally
//     disjoint from these surfaces.
//
//  3. Name-cue veto. Even with an FP signal, a token is KEPT if a name-context
//     cue (an honorific title, or a name-attribution verb) sits in the local
//     window. Over-keeping only forgoes an FP; it can never cause a leak.
//
// Deliberately NOT covered (and why): the dominant live FP class is bare
// discourse / common words in PROSE ("Please help…", "Male", "Kindly") that
// are followed by a space/comma/period exactly like a real bare first name.
// No generalisable structural signal separates those two, so suppressing them
// would drop real names → a leak. That residual is owned by the user-driven
// leak policy / allow-list (language-agnostic, user-controlled), NOT by an
// enumerated common-word lexicon.

// nameCorroborationCues is a SMALL, structural set of name-context signals:
// honorific titles that precede a name, and attribution verbs that follow one.
// It is a name-CONTEXT vocabulary, not an enumeration of names or non-names —
// each entry generalises across every name it can introduce. Multilingual on
// purpose (anonde is global); the set stays intentionally tight. Matched
// word-boundary-aware and case-insensitively via hasLocalContextCue.
var nameCorroborationCues = []string{
	// ── Honorific titles (precede a name) ─────────────────────────────
	// English
	"mr", "mr.", "mrs", "mrs.", "ms", "ms.", "mx", "mx.", "miss", "mister",
	"madam", "madame", "sir", "dr", "dr.", "prof", "prof.", "professor",
	"doctor", "rev", "rev.", "hon", "capt", "lt", "sgt", "col", "gen",
	"lord", "lady",
	// German
	"herr", "frau", "hr.", "fr.", "hrn.", "frn.", "herrn",
	// Romance (es / fr / it / pt). Deliberately excludes "don" / "doña" /
	// "dona": they collide with the common English name/word "Don"/"Donna"
	// and the contraction "Don't", so they would over-keep FPs. Dropping them
	// only forgoes an occasional keep — never a leak.
	"señor", "señora", "sr.", "sra.", "srta.",
	"monsieur", "mme", "mme.", "mlle", "signor", "signora", "senhor",
	"senhora",
	// ── Attribution verbs (follow a name) ─────────────────────────────
	"said", "says", "wrote", "writes", "told", "asked", "replied",
	"reported", "signed", "stated",
	"sagte", "schrieb", "sagt",
	"dijo", "escribió", "firmó",
}

// singleCapToken reports whether s is a single whitespace-free token (the
// gate applies only to lone capitalised tokens; multi-word names are never
// gated). The caller only passes capitalised-led surfaces, so the leading
// shape is already guaranteed by the pattern regex.
func singleCapToken(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\n\r")
}

// englishContractionSuffix reports whether a capitalised token ends in an
// UNAMBIGUOUS English contraction clitic ("We've", "I'm", "We're", "I'd",
// "We'll", "Don't"). These clitics are a closed grammatical class and no proper
// name ends in them, so gating on them is leak-safe.
//
// DELIBERATELY EXCLUDED: "'s" and bare "'t". "'s" is the POSSESSIVE clitic
// ("Jason's", "patient Demarco's") — the bare pattern captures the apostrophe,
// so "'s" fires on real names and leaks. It is structurally indistinguishable
// from the "It's"/"Let's" contraction (same clitic on a common-word stem), so
// the FP residual ("It's", "Let's") is left to the user allow-list rather than
// risk a single possessive-name leak. Verified: dropping "'s" removes 17/18 of
// the observed ai4privacy_en leaks (see the bench transcript in the report).
func englishContractionSuffix(s string) bool {
	l := strings.ToLower(s)
	for _, suf := range []string{"'ve", "'d", "'m", "'re", "'ll", "n't"} {
		if strings.HasSuffix(l, suf) && len(l) > len(suf) {
			return true
		}
	}
	return false
}

// isASCIIDigit reports whether b is an ASCII digit.
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

// isSpaceOrTab reports whether b is an intra-line ASCII whitespace byte. Used to
// skip formatting whitespace between a JSON key's closing quote and its ':' and
// before a spaced assignment '='. Newlines are deliberately excluded so gating
// never reaches across a line boundary.
func isSpaceOrTab(b byte) bool { return b == ' ' || b == '\t' }

// hasFPStructuralContext reports whether a single capitalised token at
// text[start:end] sits on an FP-indicating STRUCTURAL surface. Each signal is
// disjoint from a prose name AND verified leak-byte-identical on ai4privacy_en:
//
//   - JSON string key: the token is DOUBLE-QUOTE wrapped and followed by ':',
//     tolerating intra-line whitespace between the closing quote and the colon
//     (`"Manager":`, `"Password" : "x"`). A quoted JSON key is a machine label,
//     never a redaction-target name. The bare "Word:" form is deliberately NOT
//     a signal — a name can be a salutation ("Jaclyn54: your reminder…"), which
//     leaked one gold span in the bench, so only the quoted-key form is gated.
//   - assignment: the token is a key before '=', tolerating intra-line
//     whitespace ("Timeout=30", "Timeout = 30"). A name is a value
//     ("user = Jason"), never the key before '='.
//   - "/<digit>" version / user-agent path ("Mozilla/5.0", "Gecko/20100101").
//   - unambiguous English contraction morphology ("We've", "I'm", "Don't").
//
// Deliberately NOT signals: a following space / comma / period / bare colon /
// possessive "'s" — every one of those occurs on real bare names in prose, so
// gating on them leaks (measured).
func hasFPStructuralContext(text string, start, end int, surface string) bool {
	if englishContractionSuffix(surface) {
		return true
	}
	if end < 0 || end > len(text) || start < 0 {
		return false
	}
	// JSON string key: `"<Token>"` followed by ':' — opening and closing double
	// quotes plus a colon. Formatted JSON may separate the closing quote from the
	// colon with spaces/tabs (`"Manager" : "value"`), so skip intra-line
	// whitespace before the colon check. Unambiguously a machine key either way.
	if start > 0 && text[start-1] == '"' && end < len(text) && text[end] == '"' {
		i := end + 1
		for i < len(text) && isSpaceOrTab(text[i]) {
			i++
		}
		if i < len(text) && text[i] == ':' {
			return true
		}
	}
	if end < len(text) {
		// assignment: the token is a key before '=' ("Timeout=30", "Timeout =
		// 30"). Skip intra-line whitespace so spaced assignment labels are gated
		// too. A name is a value ("user = Jason"), never the key before '='.
		i := end
		for i < len(text) && isSpaceOrTab(text[i]) {
			i++
		}
		if i < len(text) && text[i] == '=' {
			return true
		}
		// "/<digit>" — a version / user-agent / path segment.
		if text[end] == '/' && end+1 < len(text) && isASCIIDigit(text[end+1]) {
			return true
		}
	}
	return false
}

// hasNameCorroboration reports whether a local name-context cue (honorific or
// attribution verb) sits in the CONTEXT around text[start:end] — deliberately
// EXCLUDING the candidate token itself. Searching the token would let a JSON
// key whose surface IS a cue word ("Reported", "Signed", "Stated", "Said")
// self-corroborate and escape the structural-FP gate. Two side-windows:
//
//   - before [start-16, start): a cue PRECEDING the name ("Dr. Rose").
//   - after  [end, end+20): a cue FOLLOWING the name ("Mark said"), TRUNCATED
//     at the first structural delimiter (" : =) so an attribution verb inside a
//     JSON VALUE can never corroborate the KEY — {"Manager":"said no"} stays
//     suppressed (the '"' cuts the search before "said").
//
// Over-keeping only forgoes an FP, never causes a leak — so the prose-side
// windows stay generous; only the token itself and value-side cues are excluded.
func hasNameCorroboration(text string, start, end int) bool {
	if start < 0 || end < 0 || end > len(text) || start > end {
		return false
	}
	// Before-window — cue strictly preceding the token (afterChars = 0 keeps the
	// window at [start-16, start), excluding the token itself).
	if hasLocalContextCue(text, start, start, 16, 0, nameCorroborationCues) {
		return true
	}
	// After-window — cue strictly following the token, bounded at the first
	// structural delimiter so a value-side cue cannot corroborate a key.
	hi := end + 20
	if hi > len(text) {
		hi = len(text)
	}
	seg := strings.ToLower(text[end:hi])
	if i := strings.IndexAny(seg, "\":="); i >= 0 {
		seg = seg[:i]
	}
	for _, cue := range nameCorroborationCues {
		if hasContextCue(seg, strings.ToLower(cue)) {
			return true
		}
	}
	return false
}

// suppressAnomalyPerson is the shared corroboration gate consulted at emit
// time by the anomaly-PERSON recognizers. It reports whether a candidate
// PERSON span at text[start:end] should be suppressed as a structural-surface
// false positive. Leak-safe by construction (see file header): fires only for
// a lone capitalised token that (a) carries a positive FP structural signal
// and (b) has no local name cue.
func suppressAnomalyPerson(text string, start, end int) bool {
	if start < 0 || end > len(text) || start >= end {
		return false
	}
	surface := text[start:end]
	if !singleCapToken(surface) {
		return false
	}
	if !hasFPStructuralContext(text, start, end, surface) {
		return false
	}
	// FP signal present — keep only if a name cue corroborates it.
	return !hasNameCorroboration(text, start, end)
}
