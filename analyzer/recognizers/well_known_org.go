package recognizers

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

// WellKnownOrgRecognizer is a curated closed-list ("gazetteer") recognizer that
// deterministically tags well-known company/brand names — AI vendors first — as
// ORGANIZATION. It fills a real GLiNER gap: coined post-cutoff brands (OpenAI,
// Anthropic, Claude, …) carry no org prior, so GLiNER emits ZERO org signal on
// them (absent even at threshold 0.01, not sub-threshold). A gazetteer is the only
// reliable, recency-proof fix (one-line list edit per new vendor). It's a targeted
// patch for a high-value class (LLM/dev traffic), not general ORG recall — it
// won't catch an unlisted brand, an accepted limitation. See
// .private/engineering/gliner_org_recall_research.md.
//
// Resolver fit (verified vs analyzer/result.go, pinned by
// TestWellKnownOrgResolverConflict): it's a NON-NER recognizer, so it out-ranks a
// same-token ENAnomalyRecognizer PERSON@0.25 on pure score (different type), yet a
// real GLiNER ORGANIZATION span still wins regardless of score (NER-preference) —
// it can never clobber a correct model detection.
//
// Precision discipline: CASE-SENSITIVE to the brand form (lowercase "apple"/
// "openai" in prose never match) + WORD-BOUNDARY anchored (no match inside
// "AWSInvalidToken"/"Metadata"). Multi-word brands ("Hugging Face") are phrase-
// matched as ONE span, ordered longest-first so a phrase beats its constituent.
//
// Two tiers: Tier A (always-on) — coined brands + capitalised big-tech whose
// capitalised form is overwhelmingly the company (homographs like Apple/Amazon are
// accepted over-redaction; recall > precision). Tier B (context-gated) — ambiguous
// tokens (Claude/Gemini/Mistral/Meta/Grok/Llama/Cursor) that fire ONLY near an
// AI/tech cue, so they're not redacted in ordinary prose. Registered for "*":
// brand surfaces are language-independent and none are common German/Romance
// words, so it's a pure recall win everywhere.
type WellKnownOrgRecognizer struct{}

// NewWellKnownOrgRecognizer constructs the recognizer.
func NewWellKnownOrgRecognizer() *WellKnownOrgRecognizer { return &WellKnownOrgRecognizer{} }

// Name returns the recognizer name used in logs and conflict resolution. It is
// deliberately NOT suffixed "NERRecognizer": this is a pattern recognizer, so
// the resolver lets a real GLiNER org span out-rank it (NER-preference rule).
func (r *WellKnownOrgRecognizer) Name() string { return "WellKnownOrgRecognizer" }

// SupportedEntities returns the entity types this recognizer emits.
func (r *WellKnownOrgRecognizer) SupportedEntities() []string { return []string{"ORGANIZATION"} }

// SupportedLanguages returns "*": global brand surfaces are language-independent.
func (r *WellKnownOrgRecognizer) SupportedLanguages() []string { return []string{"*"} }

// wellKnownOrgScore is the deterministic pattern-recognizer score. Comfortably
// beats the ENAnomalyRecognizer PERSON@0.25 competitor on pure score, while the
// NER-preference rule still lets a real GLiNER ORG span win — safe at both ends.
const wellKnownOrgScore = 0.85

// wellKnownOrgCtxWindow is the half-window (chars, each side, excluding the
// matched surface) scanned for an AI/tech cue when gating a Tier-B homograph.
const wellKnownOrgCtxWindow = 64

// wellKnownOrgTierA — always-on surfaces (see the tier notes on the type).
var wellKnownOrgTierA = []string{
	// AI labs / model vendors (coined — unambiguous)
	"OpenAI", "Anthropic", "ChatGPT", "Cohere", "Perplexity", "Midjourney",
	"DeepSeek", "Groq", "Replit", "xAI",
	"DeepMind", "Google DeepMind",
	"Mistral AI", "Stability AI", "Together AI", "Scale AI",
	"Meta AI", "Meta Platforms", "Google Gemini", "Hugging Face",
	// Data / dev-infra / observability
	"Databricks", "Snowflake", "Datadog", "Sentry", "Mixpanel", "Cloudflare",
	"Salesforce", "Stripe",
	// Big tech (capitalised homographs — accepted over-redaction)
	"Microsoft", "Microsoft Azure", "Azure",
	"Amazon", "Amazon Web Services", "AWS",
	"Google", "Google Cloud",
	"Apple", "Oracle", "IBM", "Adobe", "Nvidia", "NVIDIA",
}

// wellKnownOrgTierB — context-gated ambiguous tokens (see the tier notes). Kept
// OUT of the cue set below so two Tier-B tokens can't mutually trigger with no
// real domain word present.
var wellKnownOrgTierB = []string{
	"Claude", "Gemini", "Mistral", "Grok", "Llama", "LLaMA", "Meta", "Cursor",
}

// wellKnownOrgCues — AI / LLM / dev-infra domain words + a handful of
// unambiguous brand names, matched case-insensitively on word boundaries. Curated
// conservatively: broad common words ("chat", "agent", "model" was kept but
// "ml"/"nlp"/"eval" dropped) are excluded to keep the Tier-B gate from opening in
// non-AI prose. German cues ("ki", "modell", "sprachmodell") included because the
// recognizer runs on all languages.
var wellKnownOrgCues = []string{
	// English AI/LLM domain
	"ai", "llm", "llms", "gpt", "api", "sdk",
	"model", "models", "chatbot", "chatbots",
	"assistant", "assistants", "prompt", "prompts",
	"inference", "generative", "neural", "embedding", "embeddings",
	"copilot", "hallucinate", "hallucination", "hallucinations",
	"finetune", "fine-tune", "token", "tokens",
	"benchmark", "benchmarked", "dataset", "pretrained",
	"multimodal", "transformer", "reasoning", "agentic",
	// German AI cues (recognizer runs on all languages)
	"ki", "modell", "sprachmodell",
	// Unambiguous brand co-occurrence signals
	"openai", "anthropic", "chatgpt", "deepmind", "cohere", "xai",
	"perplexity", "midjourney", "huggingface", "nvidia", "databricks",
}

var (
	wellKnownOrgTierARE = buildWellKnownOrgRE(wellKnownOrgTierA)
	wellKnownOrgTierBRE = buildWellKnownOrgRE(wellKnownOrgTierB)
	wellKnownOrgCueRE   = buildWellKnownOrgCueRE(wellKnownOrgCues)
)

// buildWellKnownOrgRE compiles a case-sensitive, word-boundary alternation from
// the surfaces. Surfaces are sorted longest-first so a multi-word phrase wins
// over its single-word prefix under RE2's leftmost-FIRST alternation semantics.
// Multi-word surfaces get flexible inner whitespace ([ \t]+); every token is
// regexp.QuoteMeta'd for safety.
func buildWellKnownOrgRE(surfaces []string) *regexp.Regexp {
	sorted := append([]string(nil), surfaces...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	parts := make([]string, len(sorted))
	for i, s := range sorted {
		words := strings.Fields(s)
		for j, w := range words {
			words[j] = regexp.QuoteMeta(w)
		}
		parts[i] = strings.Join(words, `[ \t]+`)
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(parts, "|") + `)\b`)
}

// buildWellKnownOrgCueRE compiles a case-insensitive, word-boundary alternation
// of the context cues. Order is irrelevant (we only test existence).
func buildWellKnownOrgCueRE(cues []string) *regexp.Regexp {
	parts := make([]string, len(cues))
	for i, c := range cues {
		parts[i] = regexp.QuoteMeta(c)
	}
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(parts, "|") + `)\b`)
}

// Analyze emits ORGANIZATION findings at score 0.85. Tier-A surfaces are
// always-on; Tier-B homographs fire only when a context cue is nearby AND they
// don't overlap a Tier-A span (so "Meta" inside "Meta AI" yields one span).
func (r *WellKnownOrgRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	var out []analyzer.RecognizerResult
	type span struct{ s, e int }
	var tierA []span

	// Tier A: always-on, high-confidence surfaces.
	for _, m := range wellKnownOrgTierARE.FindAllStringIndex(text, -1) {
		tierA = append(tierA, span{m[0], m[1]})
		out = append(out, analyzer.RecognizerResult{
			Start:          m[0],
			End:            m[1],
			Score:          wellKnownOrgScore,
			EntityType:     "ORGANIZATION",
			RecognizerName: r.Name(),
		})
	}

	// Tier B: context-gated homographs.
	for _, m := range wellKnownOrgTierBRE.FindAllStringIndex(text, -1) {
		start, end := m[0], m[1]
		// Drop if it overlaps a Tier-A span (e.g. "Meta" within "Meta AI" or
		// "Gemini" within "Google Gemini"); the wider Tier-A span covers it.
		overlap := false
		for _, a := range tierA {
			if start < a.e && end > a.s {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		if !wellKnownOrgHasContext(text, start, end) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          start,
			End:            end,
			Score:          wellKnownOrgScore,
			EntityType:     "ORGANIZATION",
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}

// wellKnownOrgHasContext reports whether an AI/tech context cue occurs within
// wellKnownOrgCtxWindow chars to the left or right of [start,end). The matched
// surface itself is EXCLUDED from the scan (left is [ls,start), right is
// (end,re]) so a homograph can never self-trigger and Tier-B surfaces — which
// are not in the cue set — cannot mutually trigger one another.
func wellKnownOrgHasContext(text string, start, end int) bool {
	ls := start - wellKnownOrgCtxWindow
	if ls < 0 {
		ls = 0
	}
	if start > ls && wellKnownOrgCueRE.MatchString(text[ls:start]) {
		return true
	}
	re := end + wellKnownOrgCtxWindow
	if re > len(text) {
		re = len(text)
	}
	if re > end && wellKnownOrgCueRE.MatchString(text[end:re]) {
		return true
	}
	return false
}
