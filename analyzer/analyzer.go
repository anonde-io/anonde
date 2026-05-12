package analyzer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

// AnalysisConfig configures a single Analyze call.
type AnalysisConfig struct {
	// Language to use for recognizer filtering (e.g. "en"). Defaults to "en".
	Language string
	// Entities to detect. Empty means all supported entities.
	Entities []string
	// ScoreThreshold filters results below this score. Defaults to 0.
	ScoreThreshold float64
	// RemoveConflicts removes overlapping spans, keeping the best one.
	RemoveConflicts bool
	// DisableNER skips NER-based recognizers (PERSON, LOCATION, ORGANIZATION, NRP).
	// Use when you only need pattern-based entities and want maximum throughput.
	DisableNER bool

	// AllowList drops findings whose matched substring (case-insensitive,
	// trimmed) equals any value here. Use to suppress known false positives
	// like fixture emails.
	AllowList []string
	// DenyList forces findings for the given strings even if no recognizer
	// fired. Matches are emitted with EntityType="DENY_LIST" and score 1.0.
	// Use sparingly — for code-names / brand strings that must always be
	// redacted regardless of recognizer coverage.
	DenyList []string

	// ContextEnhancement overrides defaults for the context-keyword score
	// boost. Zero values fall back to DefaultContextEnhancement().
	ContextEnhancement ContextEnhancement

	// DisableContextEnhancement turns off the context boost entirely.
	DisableContextEnhancement bool
}

// AnalyzerEngine detects PII entities in text.
type AnalyzerEngine struct {
	Registry *RecognizerRegistry

	// Reconciler, if non-nil, post-processes the candidate spans after
	// context-keyword score enhancement and before threshold filtering.
	// Typical use: gate an LLM call on borderline-confidence candidates
	// to kill false positives. See the Reconciler interface for the
	// fail-open contract.
	Reconciler Reconciler

	// Auditor, if non-nil, runs one final LLM pass on the document
	// AFTER all other stages (recognizers, reconciler, threshold,
	// conflicts). It returns ADDITIONAL findings the rest of the stack
	// missed. Fails open: on any error returns nothing, so attaching
	// an auditor cannot RAISE leak rate vs. not attaching one.
	Auditor Auditor
}

// hasCapitalisedWords returns true if the text contains at least one word that
// starts with an uppercase letter and is not the very first character — a
// necessary (not sufficient) condition for NER entities to be present.
func hasCapitalisedWords(text string) bool {
	inWord := false
	for i, r := range text {
		if unicode.IsLetter(r) {
			if !inWord && i > 0 && unicode.IsUpper(r) {
				return true
			}
			inWord = true
		} else {
			inWord = false
		}
	}
	return false
}

// isNERBasedRecognizer returns true for recognizers that depend on a
// neural model (ONNX / LLM). The patterns-only mode of the engine skips
// these to avoid loading models or making external calls.
//
// Identified by name suffix, not by the entity types emitted — regex
// and lookup-based recognizers also emit PERSON / LOCATION /
// ORGANIZATION (DEAnomalyRecognizer, DEPlaceRecognizer, the German org
// pattern recognizer, …) and skipping them in patterns-only mode would
// silently drop substantial recall. Naming convention: model-backed
// recognizers MUST have a name ending in "NERRecognizer".
func isNERBasedRecognizer(rec EntityRecognizer) bool {
	return strings.HasSuffix(rec.Name(), "NERRecognizer")
}

// usesCapitalisedTextHeuristic returns true for recognizers that rely heavily on
// case cues and can be safely skipped when no interior capitalized words exist.
//
// "NERRecognizer" historically named both the (now-removed) prose recognizer
// and the Ollama recognizer; Ollama still reports that name and benefits from
// the heuristic.
func usesCapitalisedTextHeuristic(rec EntityRecognizer) bool {
	return rec.Name() == "NERRecognizer" || rec.Name() == "HugotNERRecognizer"
}

// NewAnalyzerEngine returns an engine backed by the given registry.
func NewAnalyzerEngine(registry *RecognizerRegistry) *AnalyzerEngine {
	return &AnalyzerEngine{Registry: registry}
}

// Analyze runs all applicable recognizers against text and returns deduplicated results.
//
// Pipeline order (matching Presidio's AnalyzerEngine):
//  1. Filter recognizers by language and (optional) requested entity set.
//  2. Skip NER recognizers when DisableNER is set or no capitalised words exist.
//  3. Dispatch surviving recognizers concurrently.
//  4. Merge results.
//  5. Apply context-keyword score boost (so a weak pattern hit can clear a
//     score threshold when "phone:" / "ssn:" / etc. appears nearby).
//  6. Apply DenyList (forced findings) and AllowList (drop false positives).
//  7. Filter below ScoreThreshold.
//  8. Resolve span conflicts (keep highest score, then longest).
func (e *AnalyzerEngine) Analyze(ctx context.Context, text string, cfg AnalysisConfig) ([]RecognizerResult, error) {
	if cfg.Language == "" {
		cfg.Language = "en"
	}

	candidates := e.Registry.GetByLanguage(cfg.Language)

	hasCaps := hasCapitalisedWords(text)

	if cfg.DisableNER || !hasCaps {
		filtered := candidates[:0:0]
		for _, rec := range candidates {
			if cfg.DisableNER && isNERBasedRecognizer(rec) {
				continue
			}
			if !cfg.DisableNER && !hasCaps && usesCapitalisedTextHeuristic(rec) {
				continue
			}
			filtered = append(filtered, rec)
		}
		candidates = filtered
	}

	if len(cfg.Entities) > 0 {
		entitySet := make(map[string]struct{}, len(cfg.Entities))
		for _, en := range cfg.Entities {
			entitySet[strings.ToUpper(en)] = struct{}{}
		}
		filtered := candidates[:0]
		for _, rec := range candidates {
			for _, se := range rec.SupportedEntities() {
				if _, ok := entitySet[se]; ok {
					filtered = append(filtered, rec)
					break
				}
			}
		}
		candidates = filtered
	}

	type partial struct {
		results []RecognizerResult
		err     error
	}

	ch := make(chan partial, len(candidates))
	var wg sync.WaitGroup
	for _, rec := range candidates {
		wg.Add(1)
		go func(r EntityRecognizer) {
			defer wg.Done()
			// Defensive recovery — a misbehaving recognizer (notably
			// upstream model bindings) must not bring down the whole batch.
			// We surface the panic to the caller as a normal error.
			defer func() {
				if rec := recover(); rec != nil {
					ch <- partial{nil, fmt.Errorf("recognizer %s panicked: %v", r.Name(), rec)}
				}
			}()
			res, err := r.Analyze(ctx, text, cfg.Entities, cfg.Language)
			ch <- partial{res, err}
		}(rec)
	}
	wg.Wait()
	close(ch)

	// Per-recognizer failures must not destroy partial results: a flaky NER
	// backend should not erase findings produced by the pattern recognizers
	// that ran successfully alongside it. Errors are aggregated and surfaced
	// only if EVERY recognizer failed.
	var (
		all      []RecognizerResult
		errs     []error
		okCount  int
	)
	for p := range ch {
		if p.err != nil {
			errs = append(errs, p.err)
			continue
		}
		okCount++
		all = append(all, p.results...)
	}
	if okCount == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all recognizers failed: %v", errs[0])
	}

	// 5. Context-keyword score enhancement.
	if !cfg.DisableContextEnhancement && len(all) > 0 {
		ctxCfg := cfg.ContextEnhancement
		if ctxCfg.WindowChars == 0 && ctxCfg.Boost == 0 {
			ctxCfg = DefaultContextEnhancement()
		}
		keywords := CollectContextKeywords(candidates)
		all = EnhanceWithContext(text, all, keywords, ctxCfg)
	}

	// 5a. Reconciler (optional). Gated LLM disambiguation on borderline
	// candidates. Fail-open contract: on error we keep the original
	// candidates, so the reconciler can never raise leak rate.
	if e.Reconciler != nil && len(all) > 0 {
		reconciled, err := e.Reconciler.Reconcile(ctx, text, all)
		if err == nil {
			all = reconciled
		}
	}

	// 6. DenyList (forced redaction) and AllowList (drop false positives).
	if len(cfg.DenyList) > 0 {
		all = append(all, scanDenyList(text, cfg.DenyList)...)
	}
	if len(cfg.AllowList) > 0 {
		all = applyAllowList(text, all, cfg.AllowList)
	}

	// 7. ScoreThreshold filter.
	if cfg.ScoreThreshold > 0 {
		filtered := all[:0]
		for _, r := range all {
			if r.Score >= cfg.ScoreThreshold {
				filtered = append(filtered, r)
			}
		}
		all = filtered
	}

	// 8. Conflict resolution.
	if cfg.RemoveConflicts {
		all = RemoveConflicts(all)
	} else {
		SortResults(all)
	}

	// 9. Final-audit-pass auditor (optional, recall-focused). Appends
	// any PII the rest of the pipeline missed. Fail-open in the recall
	// direction: errors return nothing, never modify existing findings.
	if e.Auditor != nil {
		extra, err := e.Auditor.Audit(ctx, text, all)
		if err == nil && len(extra) > 0 {
			all = append(all, extra...)
			if cfg.RemoveConflicts {
				all = RemoveConflicts(all)
			} else {
				SortResults(all)
			}
		}
	}

	return all, nil
}

// scanDenyList returns RecognizerResult for every case-insensitive occurrence
// of any denylisted string in text. Each finding lands at score 1.0 and is
// tagged DENY_LIST so callers can route it to a default operator.
func scanDenyList(text string, deny []string) []RecognizerResult {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	var out []RecognizerResult
	for _, raw := range deny {
		needle := strings.ToLower(strings.TrimSpace(raw))
		if needle == "" {
			continue
		}
		idx := 0
		for idx < len(lower) {
			i := strings.Index(lower[idx:], needle)
			if i < 0 {
				break
			}
			pos := idx + i
			out = append(out, RecognizerResult{
				Start:          pos,
				End:            pos + len(needle),
				Score:          1.0,
				EntityType:     "DENY_LIST",
				RecognizerName: "DenyListScanner",
			})
			idx = pos + len(needle)
		}
	}
	return out
}

// applyAllowList drops findings whose matched substring (trimmed,
// case-insensitive) equals any allowlisted value.
func applyAllowList(text string, results []RecognizerResult, allow []string) []RecognizerResult {
	if len(results) == 0 {
		return results
	}
	allowed := make(map[string]struct{}, len(allow))
	for _, a := range allow {
		allowed[strings.ToLower(strings.TrimSpace(a))] = struct{}{}
	}
	out := results[:0]
	for _, r := range results {
		if r.Start < 0 || r.End > len(text) || r.Start > r.End {
			continue
		}
		match := strings.ToLower(strings.TrimSpace(text[r.Start:r.End]))
		if _, skip := allowed[match]; skip {
			continue
		}
		out = append(out, r)
	}
	return out
}
