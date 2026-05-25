package analyzer

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/anonde-io/anonde/internal/metrics"
)

// ansiEscapeRE matches ANSI SGR / CSI sequences that real-world log
// pipelines insert into text for coloring and cursor control. Examples:
//
//	\x1b[31m   set foreground red
//	\x1b[0m    reset
//	\x1b[1;33m bold yellow
//
// We strip these in-place before dispatching recognizers so a PII span
// like "26.03.2003" doesn't get split by an escape sequence into
// fragments no pattern can match. After recognizers run, finding
// offsets are translated back to the original text via an offset map.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// stripANSI removes ANSI escape sequences from text and returns the
// cleaned text plus an offset map. The map is indexed by position in
// the cleaned text and yields the corresponding position in the
// original. For a stripped-text span [s, e) the original-text span is
// [offsetMap[s], offsetMap[e-1]+1).
//
// On text with no ANSI codes the result is a no-op: cleaned == text
// and offsetMap is identity. Cost on clean input is one regex scan
// per call.
func stripANSI(text string) (cleaned string, offsetMap []int) {
	matches := ansiEscapeRE.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	var b strings.Builder
	b.Grow(len(text))
	offsetMap = make([]int, 0, len(text))
	prev := 0
	for _, m := range matches {
		// Append text segment before the escape.
		segment := text[prev:m[0]]
		b.WriteString(segment)
		for i := prev; i < m[0]; i++ {
			offsetMap = append(offsetMap, i)
		}
		prev = m[1]
	}
	// Append the tail after the last escape.
	if prev < len(text) {
		b.WriteString(text[prev:])
		for i := prev; i < len(text); i++ {
			offsetMap = append(offsetMap, i)
		}
	}
	return b.String(), offsetMap
}

// translateFindings remaps Start/End offsets from cleaned-text space
// to original-text space using the offset map produced by stripANSI.
// A nil offsetMap is a no-op — used when stripANSI found no escapes.
func translateFindings(findings []RecognizerResult, offsetMap []int) []RecognizerResult {
	if offsetMap == nil {
		return findings
	}
	out := findings[:0]
	for _, r := range findings {
		if r.Start < 0 || r.End <= r.Start || r.End > len(offsetMap) {
			// Out-of-range finding (shouldn't happen but is defensive).
			continue
		}
		origStart := offsetMap[r.Start]
		origEnd := offsetMap[r.End-1] + 1
		r.Start = origStart
		r.End = origEnd
		out = append(out, r)
	}
	return out
}

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
	// DisableNER skips model-backed NER recognizers (recognizers whose name
	// ends in "NERRecognizer", i.e. HugotNERRecognizer / OllamaNERRecognizer).
	// Use when you want maximum throughput and don't need a neural model
	// loaded or called.
	//
	// Pattern-based PERSON / LOCATION / ORGANIZATION recognizers
	// (ENAnomalyRecognizer, DEAnomalyRecognizer, DEPlaceRecognizer, …) are
	// NOT gated by this flag — they're cheap regex / vocabulary work and
	// silently disabling them under DisableNER would drop substantial
	// recall on the patterns-only deployment path.
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

	// metrics records per-finding and per-conflict observations. Nil
	// is safe — the analyzer treats it as a no-op. Set via
	// SetMetrics(r) from cmd/anonde wiring after the engine is built;
	// it's separate from the constructor because the constructor sees
	// fan-out through many helper functions (DefaultAnalyzerEngine,
	// DefaultAnalyzerEngineWithGLiNERConfig, etc.) and a SetMetrics
	// hook keeps that fan-out unchanged.
	metrics metrics.Recorder
}

// SetMetrics attaches a metrics Recorder for per-finding and
// per-conflict instrumentation. Pass metrics.NewNoop() to explicitly
// disable, or just don't call this — nil and noop behave identically.
// Safe to call once at boot; not goroutine-safe to reassign at
// runtime (no use case for that yet).
func (e *AnalyzerEngine) SetMetrics(r metrics.Recorder) {
	e.metrics = r
}

// recorder returns the analyzer's Recorder, never nil.
func (e *AnalyzerEngine) recorder() metrics.Recorder {
	if e.metrics == nil {
		return metrics.NewNoop()
	}
	return e.metrics
}

// conflictKind buckets a RecognizerResult into one of the two
// metric labels for anonde_conflicts_resolved_total: "ner" for
// findings produced by a model-backed recognizer, "pattern" for
// everything else (regex, checksum, vocabulary). Keeps the conflict
// metric's cardinality at 2×2 — the alternative of labelling by
// recognizer name would explode to 52×52.
func conflictKind(r RecognizerResult) string {
	if isNERRecognizer(r) {
		return "ner"
	}
	return "pattern"
}

// hasCapitalisedWords returns true if the text contains at least one word that
// starts with an uppercase letter — a necessary (not sufficient) condition for
// NER entities to be present. We deliberately accept a leading-position capital:
// short inputs like a single CSV cell ("John Smith"), a log line ("Smith called
// support"), or any sentence where the only entity is the first token would
// otherwise be silently skipped by NER and produce zero PERSON findings.
func hasCapitalisedWords(text string) bool {
	inWord := false
	for _, r := range text {
		if unicode.IsLetter(r) {
			if !inWord && unicode.IsUpper(r) {
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

	// Strip ANSI escape sequences before recognizers run, so log-style
	// inputs with embedded color codes are processed as plain text.
	// findings emitted by recognizers reference positions in cleanText,
	// translated back to the original at the end of the pipeline before
	// returning to the caller. On plain text with no escapes this is a
	// zero-cost no-op (offsetMap == nil).
	originalText := text
	cleanText, offsetMap := stripANSI(text)
	text = cleanText

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
	// Wait-or-cancel: spawn a sentinel goroutine that closes `done`
	// once every recognizer has returned. If `ctx` fires first we stop
	// blocking and harvest whatever results landed on the buffered
	// channel — partial coverage is still useful (the bench scores
	// per-recognizer leaks, and an HTTP caller that just timed out
	// would rather have something than nothing).
	//
	// Recognizers SHOULD honour ctx in their inner loops (GLiNER chunk
	// loop, Ollama HTTP, etc.); the select-on-ctx here is the bounded
	// guard against the ones that don't — a single hung recognizer
	// would otherwise stall the whole pipeline forever.
	//
	// Note: `ch` is buffered to len(candidates), so the laggard
	// goroutines can still send their result after we've returned and
	// will not leak — they exit on their own.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	var ctxErr error
	select {
	case <-done:
		close(ch)
	case <-ctx.Done():
		ctxErr = ctx.Err()
		log.Printf("analyzer: ctx cancelled before all recognizers returned (%v); harvesting partial results", ctxErr)
		// Do NOT close ch — slow goroutines still hold a send token
		// and would panic on send-to-closed. Drain non-blocking by
		// switching to a different receive pattern below.
	}

	// Per-recognizer failures must not destroy partial results: a flaky NER
	// backend should not erase findings produced by the pattern recognizers
	// that ran successfully alongside it. Errors are aggregated and surfaced
	// only if EVERY recognizer failed.
	var (
		all     []RecognizerResult
		errs    []error
		okCount int
	)
	// Two drain modes:
	//   * ctxErr == nil: `ch` is closed; classic range drains all results.
	//   * ctxErr != nil: `ch` is NOT closed (slow goroutines may still
	//     send later); non-blocking drain collects whatever is in the
	//     buffer right now and bails.
	if ctxErr == nil {
		for p := range ch {
			if p.err != nil {
				log.Printf("analyzer: recognizer error (swallowed): %v", p.err)
				errs = append(errs, p.err)
				continue
			}
			okCount++
			all = append(all, p.results...)
		}
	} else {
	drain:
		for {
			select {
			case p := <-ch:
				if p.err != nil {
					log.Printf("analyzer: recognizer error (swallowed): %v", p.err)
					errs = append(errs, p.err)
					continue
				}
				okCount++
				all = append(all, p.results...)
			default:
				break drain
			}
		}
	}
	if okCount == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all recognizers failed: %v", errs[0])
	}
	// If ctx is cancelled and we have nothing to show, surface the ctx
	// error so the caller can distinguish "no PII" from "we gave up".
	if ctxErr != nil && okCount == 0 {
		return nil, ctxErr
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

	// 8. Conflict resolution. Wire the metrics callback so
	// anonde_conflicts_resolved_total{winner_kind, loser_kind}
	// tracks pattern-vs-NER arbitration in production. Cardinality is
	// 2×2 since the kinds collapse to {"ner","pattern"} regardless of
	// recognizer name.
	rec := e.recorder()
	conflictCB := func(winner, loser RecognizerResult) {
		rec.ConflictResolved(conflictKind(winner), conflictKind(loser))
	}
	if cfg.RemoveConflicts {
		all = RemoveConflictsWithCallback(all, conflictCB)
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
				all = RemoveConflictsWithCallback(all, conflictCB)
			} else {
				SortResults(all)
			}
		}
	}

	// Emit per-finding metrics over the surviving set. Done here (vs.
	// upstream of conflict resolution) because we want the
	// score histogram + entities counter to reflect what the
	// anonymizer actually tokenizes — losers in a conflict don't
	// show up downstream and shouldn't be double-counted as
	// "detected".
	for _, r := range all {
		rec.EntityDetected(r.EntityType, r.RecognizerName, r.Score)
	}

	// 10. Translate findings from cleanText offsets back to the
	// original input. No-op when stripANSI found no escape sequences
	// (offsetMap == nil). Must be the last step so every downstream
	// consumer (anonymizer, vault, audit log) sees positions that
	// match the caller-supplied text exactly.
	_ = originalText // retained for clarity; the caller owns the slice.
	all = translateFindings(all, offsetMap)

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
