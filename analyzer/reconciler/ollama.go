// Package reconciler hosts implementations of analyzer.Reconciler — the
// post-processing stage that gates an LLM call on borderline-confidence PII
// candidates and decides whether to keep or drop each one.
//
// Ollama is the only backend today (per the "Ollama-local only" product
// constraint). The interface lives in analyzer/ so additional backends can
// be added without changing call-site wiring.
package reconciler

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

	"github.com/anonde-io/anonde/analyzer"
)

// defaultOllamaCacheSize bounds the in-process decision cache for the
// reconciler. The cache key is (span surface form, entity type, hash of
// the surrounding ±TextWindow chars), so cardinality grows with
// document throughput. The old unbounded `map[string]bool` leaked
// memory in long-running production processes; the LRU bounds RSS at
// roughly cacheSize × (key + bool) ≈ 100k * ~80 B = ~8 MB.
//
// Override via ANONDE_OLLAMA_CACHE_SIZE; values <= 0 fall back to this
// default, malformed values are logged and ignored (matching the
// GLINER_POOL_SIZE precedent — a typo never blocks boot).
const defaultOllamaCacheSize = 100_000

// OllamaConfig configures the Ollama-backed reconciler. Zero values are
// replaced with sensible defaults in NewOllama.
type OllamaConfig struct {
	// Endpoint is the Ollama HTTP base URL.
	// Default: "http://localhost:11434".
	Endpoint string

	// Model is the Ollama model tag to call.
	// Default: "llama3.2:3b" (small, multilingual, fast on CPU).
	Model string

	// LowGate: candidates with score < LowGate are dropped without
	// consulting the LLM. Default: 0.40.
	LowGate float64

	// HighGate: candidates with score >= HighGate are kept without
	// consulting the LLM. Default: 0.85.
	HighGate float64

	// MaxConcurrent bounds in-flight LLM requests per Reconcile call.
	// Default: 4.
	MaxConcurrent int

	// Timeout is the per-span LLM call deadline.
	// Default: 5 seconds.
	Timeout time.Duration

	// TextWindow is the number of chars of surrounding context fed to
	// the model. Default: 200.
	TextWindow int
}

// Ollama implements analyzer.Reconciler against a local Ollama server.
type Ollama struct {
	cfg    OllamaConfig
	client *http.Client

	// cache memoises decisions by (span-text, type, surrounding context
	// hash). Bounded LRU — previously an unbounded `map[string]bool`
	// that leaked memory in long-running processes. Cache key shape:
	// `entityType|span|sha1(window)[:16hex]` (see cacheKey()).
	// Capacity is set at construction time from
	// ANONDE_OLLAMA_CACHE_SIZE (default 100k entries, ~8 MB RSS).
	cache *lru.Cache[string, bool]

	// group deduplicates in-flight identical requests so two parallel
	// workers don't both hit the LLM with the same prompt.
	group singleflight.Group

	// Per-process statistics. Exposed via Stats() so callers (bench
	// runners, production telemetry) can see what the reconciler did.
	stats reconcilerStats
}

// Stats are cumulative reconciler counters since process start.
type Stats struct {
	// Total candidates seen across all Reconcile calls.
	Total int64
	// Dropped without an LLM call because score < LowGate.
	DroppedLow int64
	// Kept without an LLM call because score >= HighGate.
	KeptHigh int64
	// Sent to the LLM (score in [LowGate, HighGate)).
	LLMBand int64
	// Subset of LLMBand: LLM voted KEEP and the candidate was kept.
	LLMKeep int64
	// Subset of LLMBand: LLM voted DROP and the candidate was removed.
	LLMDrop int64
	// Subset of LLMBand: LLM call errored or timed out — fail-open kept
	// the candidate, equivalent to LLMKeep for the output but logged
	// separately so operators can see how often the model is reachable.
	LLMError int64
	// Cache hits (no LLM call made because the same (span,type,window)
	// already had a decision).
	CacheHit int64
}

type reconcilerStats struct {
	total      int64
	droppedLow int64
	keptHigh   int64
	llmBand    int64
	llmKeep    int64
	llmDrop    int64
	llmError   int64
	cacheHit   int64
}

// Stats returns a snapshot of cumulative counters.
func (r *Ollama) Stats() Stats {
	return Stats{
		Total:      atomicLoad(&r.stats.total),
		DroppedLow: atomicLoad(&r.stats.droppedLow),
		KeptHigh:   atomicLoad(&r.stats.keptHigh),
		LLMBand:    atomicLoad(&r.stats.llmBand),
		LLMKeep:    atomicLoad(&r.stats.llmKeep),
		LLMDrop:    atomicLoad(&r.stats.llmDrop),
		LLMError:   atomicLoad(&r.stats.llmError),
		CacheHit:   atomicLoad(&r.stats.cacheHit),
	}
}

// NewOllama constructs an Ollama reconciler with the given config.
// Defaults are filled in for any zero-valued fields.
func NewOllama(cfg OllamaConfig) *Ollama {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.2:3b"
	}
	if cfg.LowGate == 0 {
		cfg.LowGate = 0.40
	}
	if cfg.HighGate == 0 {
		cfg.HighGate = 0.85
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.TextWindow == 0 {
		cfg.TextWindow = 200
	}
	cacheSize := defaultOllamaCacheSize
	if raw := strings.TrimSpace(os.Getenv("ANONDE_OLLAMA_CACHE_SIZE")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cacheSize = n
		}
	}
	cache, err := lru.New[string, bool](cacheSize)
	if err != nil {
		// lru.New only errors on size<=0; the parse above filters that.
		// Fall back to the default and keep moving — a misconfigured env
		// var must not crash startup.
		cache, _ = lru.New[string, bool](defaultOllamaCacheSize)
	}
	return &Ollama{
		cfg:    cfg,
		client: &http.Client{},
		cache:  cache,
	}
}

// Reconcile partitions candidates by score, consults the LLM on the middle
// band, and returns the kept set. Fail-open: any LLM error or timeout
// causes the candidate to be kept, preserving the no-reconciler leak rate.
func (r *Ollama) Reconcile(ctx context.Context, text string, candidates []analyzer.RecognizerResult) ([]analyzer.RecognizerResult, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}

	out := make([]analyzer.RecognizerResult, 0, len(candidates))
	type ambig struct {
		idx int
		res analyzer.RecognizerResult
	}
	var ambiguous []ambig

	for i, c := range candidates {
		atomicAdd(&r.stats.total, 1)
		switch {
		case c.Score >= r.cfg.HighGate:
			atomicAdd(&r.stats.keptHigh, 1)
			out = append(out, c)
		case c.Score < r.cfg.LowGate:
			atomicAdd(&r.stats.droppedLow, 1)
		default:
			atomicAdd(&r.stats.llmBand, 1)
			ambiguous = append(ambiguous, ambig{i, c})
		}
	}
	if len(ambiguous) == 0 {
		return out, nil
	}

	// Fan-out with bounded concurrency. decisions[j] true = keep.
	decisions := make([]bool, len(ambiguous))
	sem := make(chan struct{}, r.cfg.MaxConcurrent)
	var wg sync.WaitGroup

	for j, a := range ambiguous {
		wg.Add(1)
		sem <- struct{}{}
		go func(j int, a ambig) {
			defer wg.Done()
			defer func() { <-sem }()
			decisions[j] = r.askKeep(ctx, text, a.res)
		}(j, a)
	}
	wg.Wait()

	for j, a := range ambiguous {
		if decisions[j] {
			out = append(out, a.res)
		}
	}
	return out, nil
}

// askKeep returns true if the LLM votes KEEP or any error occurs (fail-open).
func (r *Ollama) askKeep(ctx context.Context, text string, c analyzer.RecognizerResult) bool {
	if c.Start < 0 || c.End > len(text) || c.Start >= c.End {
		return true // malformed; let downstream handle
	}
	spanText := text[c.Start:c.End]
	window := windowAround(text, c.Start, c.End, r.cfg.TextWindow)

	key := cacheKey(spanText, c.EntityType, window)
	if v, ok := r.cache.Get(key); ok {
		atomicAdd(&r.stats.cacheHit, 1)
		if v {
			atomicAdd(&r.stats.llmKeep, 1)
		} else {
			atomicAdd(&r.stats.llmDrop, 1)
		}
		return v
	}

	// singleflight collapses concurrent identical requests into one
	// LLM call. Without it, K parallel workers handed the same span
	// (e.g. "Bob" appearing K times in one doc) would each fire an HTTP
	// request even when the answer is identical.
	v, err, _ := r.group.Do(key, func() (any, error) {
		callCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
		defer cancel()
		keep, callErr := r.callOllama(callCtx, window, spanText, c.EntityType)
		if callErr != nil {
			return nil, callErr
		}
		r.cache.Add(key, keep)
		return keep, nil
	})
	if err != nil {
		// Fail-open: keep candidate on any error.
		atomicAdd(&r.stats.llmError, 1)
		return true
	}
	keep := v.(bool)
	if keep {
		atomicAdd(&r.stats.llmKeep, 1)
	} else {
		atomicAdd(&r.stats.llmDrop, 1)
	}
	return keep
}

func atomicAdd(p *int64, n int64) { atomic.AddInt64(p, n) }
func atomicLoad(p *int64) int64   { return atomic.LoadInt64(p) }

const reconcilerSystemPrompt = `You verify whether a substring of clinical text is genuine personally identifiable information (PII).
Reply with exactly one word: KEEP or DROP.
- KEEP if the span is real PII of the suggested type.
- DROP if it is not PII, or if it is the wrong type (e.g. a lab value labelled as PHONE).
Do not explain. One word only.`

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
}

func (r *Ollama) callOllama(ctx context.Context, window, span, entityType string) (bool, error) {
	userMsg := fmt.Sprintf(
		"Text:\n\"\"\"\n%s\n\"\"\"\n\nCandidate span: %q\nSuggested PII type: %s\n\nIs the span genuine %s? KEEP or DROP.",
		window, span, entityType, entityType,
	)

	req := ollamaRequest{
		Model:  r.cfg.Model,
		Stream: false,
		Messages: []ollamaMessage{
			{Role: "system", Content: reconcilerSystemPrompt},
			{Role: "user", Content: userMsg},
		},
		// Constrain output: low temperature, short max-tokens. Local
		// model implementations may ignore these; that is fine.
		Options: map[string]any{
			"temperature": 0,
			"num_predict": 4,
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return false, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.Endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, b)
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return parseKeepDrop(out.Message.Content), nil
}

// parseKeepDrop returns true if the model's reply starts with "K" (KEEP).
// Defaults to true (fail-open) on garbled output. This is intentional —
// the reconciler MUST NOT raise leak rate vs not running at all.
func parseKeepDrop(reply string) bool {
	for _, line := range strings.Split(reply, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		upper := strings.ToUpper(s)
		switch {
		case strings.HasPrefix(upper, "DROP"), strings.HasPrefix(upper, "NO"):
			return false
		case strings.HasPrefix(upper, "KEEP"), strings.HasPrefix(upper, "YES"):
			return true
		}
	}
	return true // fail-open
}

// windowAround returns ±window chars around [start,end) on the text.
func windowAround(text string, start, end, window int) string {
	return text[max(start-window, 0):min(end+window, len(text))]
}

// cacheKey builds a stable key from span surface form, entity type, and a
// hash of the surrounding window. The window hash is included so two
// occurrences of the same span in different contexts can disagree.
func cacheKey(span, entityType, window string) string {
	h := sha1.Sum([]byte(window))
	return entityType + "|" + span + "|" + hex.EncodeToString(h[:8])
}
