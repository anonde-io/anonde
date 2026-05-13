//go:build hugot

// ner_gliner.go runs a GLiNER ONNX export end-to-end inside the Go process.
// Tokenisation is pure Go (gomlx/go-huggingface/tokenizers/hftokenizer);
// model execution goes through yalue/onnxruntime_go, which is a CGO wrapper
// around libonnxruntime. Pairing them gets us the same span quality as the
// Python sidecar (bench/runners/gliner.py) without spinning up a Python
// process per inference batch.
//
// CGO contract
// ------------
// This file is only compiled with `-tags hugot` AND CGO_ENABLED=1. yalue's
// source has `//go:build cgo` constraints baked in; without CGO every Go
// file in the package is excluded and the linker fails with
// "undefined: ort.*". The default (no-tag) build picks up ner_gliner_off.go
// which raises a clear error at Analyze-time. Production deployments must
// run a CGO build and ship libonnxruntime.{so,dylib} alongside the binary.
//
// Compatibility with the Python sidecar (bench/runners/gliner.py): same
// model id (knowledgator/gliner-pii-base-v1.0) and same canonical-entity
// mapping. The sidecar exists for parity-check (kept in the bench matrix
// as `gliner-py`) not as the production path — production is this file.
//
// Backend lifecycle
// -----------------
// `ort.InitializeEnvironment()` is process-wide and must be called exactly
// once. We guard it behind a sync.Once. `ort.SetSharedLibraryPath()` must
// fire BEFORE InitializeEnvironment, so callers wanting a non-default
// libonnxruntime location set GLiNERConfig.SharedLibraryPath on the FIRST
// recognizer they construct — later changes are ignored (the env is
// already up). The `--ort-library` flag on bench/runners/anonde.go
// is the usual way to plumb this through.
//
// Session lifecycle
// -----------------
// Each recognizer owns one `*ort.DynamicAdvancedSession` covering its
// (model, label-set) pair. Sessions are not safe for concurrent use: every
// Run() walks the input/output slot tables on the *AdvancedSession struct.
// We serialise Analyze() calls with a per-recognizer mutex; if you want
// parallelism, spin up N recognizers. The session is destroyed by Destroy().
//
// Tensor handling
// ---------------
// onnxruntime takes arbitrary input shapes, so unlike the prior
// gomlx/simplego port we don't pad to maxTokens. Per-chunk tensors are
// freshly allocated, fed to Run(), and Destroy()'d on return. The output
// tensor is also auto-allocated by onnxruntime (passing a nil slot tells
// the runtime to figure out the shape) — we read the float32 buffer back,
// destroy the tensor, and decode.
//
// Why not BiEncoder / unified API
// -------------------------------
// The pii-base v1.0 export is uni-encoder span. Inputs are 6 named tensors
// (input_ids, attention_mask, words_mask, text_lengths, span_idx,
// span_mask). The output is a single "logits" tensor of shape
// [B, L, K, C]. Swapping in a different GLiNER variant (bi-encoder,
// flat-decoder, sequence-tag) requires changing the input list and the
// decoder — out of scope here.

package recognizers

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
	"github.com/knights-analytics/hugot"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/anonde-io/anonde/analyzer"
)

// Hard-coded model parameters. The Python sidecar uses identical
// defaults; override via GLiNERConfig if you swap to a model with
// different settings.
const (
	// Prompt token literals. The model tokenizer's `added_tokens`
	// recognises these exactly and emits the trained ENT/SEP IDs
	// (128001 / 128002 in gliner-pii-base-v1.0).
	gliner_entToken = "<<ENT>>"
	gliner_sepToken = "<<SEP>>"

	defaultGLiNERThreshold = 0.40
	defaultGLiNERMaxWidth  = 12
	defaultGLiNERMaxTokens = 384

	// Sliding-window chunking defaults. Chosen so prompt (~110 tokens
	// for the 34-label default set) + content + specials stays under
	// MaxTokens=384 on typical German clinical text (~4 chars/token).
	defaultGLiNERChunkChars   = 1200
	defaultGLiNERChunkOverlap = 200
)

// glinerWordRegex mirrors GLiNER's WhitespaceTokenSplitter:
//
//	r"\w+(?:[-_]\w+)*|\S"
//
// Python `\w` for str patterns is Unicode-aware (letters / digits /
// underscore from any script). Go's `regexp` `\w` is ASCII-only, so we
// expand to explicit Unicode classes. The `|\S` alternation catches
// isolated punctuation as one-character "words" — same behaviour as
// Python.
var glinerWordRegex = regexp.MustCompile(`[\p{L}\p{N}_]+(?:[-_][\p{L}\p{N}_]+)*|\S`)

// GLiNERDebug, when set true, writes token-level diagnostics to
// `gliner_debug.log` in the current working directory on every chunk
// inference. Intentionally exported so a small driver (e.g.
// cmd/gliner_probe) can flip it without env vars.
var GLiNERDebug = false

// debugLog is the diagnostic sink used by GLiNERRecognizer when
// GLiNERDebug is true. It appends to ./gliner_debug.log. Failures are
// swallowed silently — diagnostics must never break inference.
func debugLog(format string, args ...any) {
	f, err := os.OpenFile("./gliner_debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[gliner] "+format, args...)
}

// glinerOrtOnce gates the one-time onnxruntime environment init. The
// first recognizer to call init() wins the race and decides the shared
// library path; subsequent recognizers with a different
// SharedLibraryPath have their value silently ignored — onnxruntime
// only takes the path before InitializeEnvironment.
var (
	glinerOrtOnce    sync.Once
	glinerOrtInitErr error
)

// initOrtEnvironment performs the one-time SetSharedLibraryPath +
// InitializeEnvironment dance. Safe to call from multiple goroutines;
// only the first call has any effect.
func initOrtEnvironment(libPath string) error {
	glinerOrtOnce.Do(func() {
		if libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		if err := ort.InitializeEnvironment(); err != nil {
			glinerOrtInitErr = fmt.Errorf("gliner: initialize onnxruntime environment: %w", err)
		}
	})
	return glinerOrtInitErr
}

// GLiNERRecognizer runs a GLiNER ONNX export to detect PII entities.
// GLiNER is an open-set NER architecture — the label set is supplied
// at inference time via the prompt, so the same loaded model can score
// arbitrary entity types without retraining.
//
// Naming: ends in "NERRecognizer" so the analyzer engine's DisableNER
// flag silences it alongside HugotNERRecognizer.
type GLiNERRecognizer struct {
	cfg GLiNERConfig

	once    sync.Once
	initErr error

	tokenizer *hftokenizer.Tokenizer

	// onnxruntime session for this recognizer's (model, label-set).
	// Each Run() call serialises on `mu` because yalue's
	// DynamicAdvancedSession internally caches the input/output OrtValue
	// pointers on the session struct.
	session *ort.DynamicAdvancedSession
	mu      sync.Mutex

	// ONNX I/O names captured at init time. Useful for diagnostics and
	// kept in sync with the Python collator's input dict.
	onnxInputNames []string
	onnxOutputName string

	modelPath string

	// Resolved at init.
	labels        []string
	labelToEntity map[string]string
	threshold     float64
	maxWidth      int
	maxTokens     int

	// Pre-computed prompt-prefix metadata. The prompt is constant
	// across all inputs, so its byte length is enough to filter
	// prompt tokens from text tokens by their char offset.
	promptString     string
	promptCharLength int
}

// NewGLiNERRecognizer constructs a recognizer with the given config. A
// zero-value config selects knowledgator/gliner-pii-base-v1.0 with the
// default PII label set.
func NewGLiNERRecognizer(cfg GLiNERConfig) *GLiNERRecognizer {
	if cfg.ModelsDir == "" {
		home, _ := os.UserHomeDir()
		cfg.ModelsDir = filepath.Join(home, ".cache", "anonde", "models")
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "knowledgator/gliner-pii-base-v1.0"
		// The repo ships several ONNX variants; the uint8-quantised
		// build is the speed/size sweet spot for CPU inference.
		if cfg.OnnxFilePath == "" {
			cfg.OnnxFilePath = "onnx/model_quint8.onnx"
		}
	}
	return &GLiNERRecognizer{cfg: cfg}
}

// Name returns the recognizer name. MUST end in "NERRecognizer" so the
// DisableNER suffix-check fires.
func (r *GLiNERRecognizer) Name() string { return "GLiNERRecognizer" }

// SupportedEntities returns the deduplicated set of canonical entity
// types reachable via the label→entity map.
func (r *GLiNERRecognizer) SupportedEntities() []string {
	m := r.cfg.LabelToEntity
	if len(m) == 0 {
		m = DefaultLabelToEntity
	}
	seen := make(map[string]struct{}, len(m))
	out := make([]string, 0, len(m))
	for _, v := range m {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// SupportedLanguages: GLiNER models are typically multilingual. The
// v1.0 PII model card lists EN+DE+ES+FR+IT+NL+PT — extend if you swap
// to a different ModelName.
func (r *GLiNERRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt"}
}

// init runs the one-time setup: locate model files, build the static
// label prompt, initialise the shared onnxruntime environment, and open
// a DynamicAdvancedSession over the ONNX file.
func (r *GLiNERRecognizer) init(ctx context.Context) error {
	r.once.Do(func() {
		// --- config defaults --------------------------------------
		r.labels = r.cfg.Labels
		if len(r.labels) == 0 {
			r.labels = DefaultPIILabels
		}
		r.labelToEntity = r.cfg.LabelToEntity
		if len(r.labelToEntity) == 0 {
			r.labelToEntity = DefaultLabelToEntity
		}
		r.threshold = r.cfg.Threshold
		if r.threshold == 0 {
			r.threshold = defaultGLiNERThreshold
		}
		r.maxWidth = r.cfg.MaxWidth
		if r.maxWidth <= 0 {
			r.maxWidth = defaultGLiNERMaxWidth
		}
		r.maxTokens = r.cfg.MaxTokens
		if r.maxTokens <= 0 {
			r.maxTokens = defaultGLiNERMaxTokens
		}

		// --- model files ------------------------------------------
		// Reuse hugot's on-disk layout (replaces "/" with "_") so the
		// HugotNERRecognizer and GLiNERRecognizer share a cache when
		// the same repo backs both.
		log.Printf("gliner: init starting model=%s onnx_file=%s models_dir=%s labels=%d threshold=%.2f auto_download=%v",
			r.cfg.ModelName, r.cfg.OnnxFilePath, r.cfg.ModelsDir,
			len(r.labels), r.threshold, r.cfg.AutoDownload)

		modelPath := filepath.Join(r.cfg.ModelsDir, sanitizeModelName(r.cfg.ModelName))
		if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
			if !r.cfg.AutoDownload {
				r.initErr = fmt.Errorf("gliner: model not found at %s; set AutoDownload: true or download manually", modelPath)
				log.Printf("gliner: INIT FAILED: %v", r.initErr)
				return
			}
			if mkErr := os.MkdirAll(r.cfg.ModelsDir, 0o755); mkErr != nil {
				r.initErr = fmt.Errorf("gliner: create models dir: %w", mkErr)
				log.Printf("gliner: INIT FAILED: %v", r.initErr)
				return
			}
			log.Printf("gliner: downloading %s into %s (onnx=%s)", r.cfg.ModelName, r.cfg.ModelsDir, r.cfg.OnnxFilePath)
			downloadOpts := hugot.NewDownloadOptions()
			if r.cfg.OnnxFilePath != "" {
				downloadOpts.OnnxFilePath = r.cfg.OnnxFilePath
			}
			downloadedPath, dlErr := hugot.DownloadModel(ctx, r.cfg.ModelName, r.cfg.ModelsDir, downloadOpts)
			if dlErr != nil {
				r.initErr = fmt.Errorf("gliner: download model %s: %w", r.cfg.ModelName, dlErr)
				log.Printf("gliner: INIT FAILED: %v", r.initErr)
				return
			}
			modelPath = downloadedPath
		}
		r.modelPath = modelPath

		// --- tokenizer --------------------------------------------
		tokPath := filepath.Join(modelPath, "tokenizer.json")
		if _, err := os.Stat(tokPath); err != nil {
			r.initErr = fmt.Errorf("gliner: tokenizer.json missing under %s: %w", modelPath, err)
			log.Printf("gliner: INIT FAILED: %v", r.initErr)
			return
		}
		tok, err := hftokenizer.NewFromFile(nil, tokPath)
		if err != nil {
			r.initErr = fmt.Errorf("gliner: load tokenizer: %w", err)
			log.Printf("gliner: INIT FAILED: %v", r.initErr)
			return
		}
		// Default options request span annotations and post-
		// processor specials (the [CLS]/[SEP] wrappers).
		if err := tok.With(api.EncodeOptions{
			AddSpecialTokens:         true,
			IncludeSpans:             true,
			IncludeSpecialTokensMask: true,
		}); err != nil {
			r.initErr = fmt.Errorf("gliner: configure tokenizer: %w", err)
			return
		}
		r.tokenizer = tok

		// Pre-build the prompt prefix string.
		// Format: "<<ENT>> label1 <<ENT>> label2 ... <<SEP> word1 ..."
		// Each label gets its own <<ENT>> marker; the final <<SEP>>
		// separates the prompt from the text. A trailing space
		// ensures the SPM pre-tokenizer treats <<SEP>> and the first
		// text word as distinct.
		var sb strings.Builder
		for _, lbl := range r.labels {
			sb.WriteString(gliner_entToken)
			sb.WriteByte(' ')
			sb.WriteString(lbl)
			sb.WriteByte(' ')
		}
		sb.WriteString(gliner_sepToken)
		sb.WriteByte(' ')
		r.promptString = sb.String()
		r.promptCharLength = len(r.promptString)

		// --- onnxruntime environment ------------------------------
		if err := initOrtEnvironment(r.cfg.SharedLibraryPath); err != nil {
			r.initErr = err
			return
		}

		// --- locate the ONNX file ---------------------------------
		onnxFile := filepath.Join(modelPath, "model.onnx")
		if r.cfg.OnnxFilePath != "" {
			candidate := filepath.Join(modelPath, r.cfg.OnnxFilePath)
			if _, statErr := os.Stat(candidate); statErr == nil {
				onnxFile = candidate
			}
		}
		if _, statErr := os.Stat(onnxFile); statErr != nil {
			candidate, scanErr := findFirstOnnx(modelPath)
			if scanErr != nil || candidate == "" {
				r.initErr = fmt.Errorf("gliner: no ONNX file found under %s (set OnnxFilePath)", modelPath)
				return
			}
			onnxFile = candidate
		}

		// --- open the session -------------------------------------
		// Input/output names come from
		// gliner/onnx/model.py::UniEncoderSpanORTModel.forward. Order
		// must match the order passed to session.Run().
		r.onnxInputNames = []string{
			"input_ids",
			"attention_mask",
			"words_mask",
			"text_lengths",
			"span_idx",
			"span_mask",
		}
		r.onnxOutputName = "logits"

		session, sessErr := ort.NewDynamicAdvancedSession(
			onnxFile,
			r.onnxInputNames,
			[]string{r.onnxOutputName},
			nil, // default session options
		)
		if sessErr != nil {
			r.initErr = fmt.Errorf("gliner: open onnx session %s: %w", onnxFile, sessErr)
			log.Printf("gliner: INIT FAILED: %v", r.initErr)
			return
		}
		r.session = session
		log.Printf("gliner: ready model_path=%s onnx=%s tokens_max=%d width_max=%d",
			modelPath, onnxFile, r.maxTokens, r.maxWidth)
	})
	return r.initErr
}

// findFirstOnnx walks the given dir and returns the first *.onnx file
// it finds (deterministic walk order — sorted directory listing). Used
// as a fallback when neither cfg.OnnxFilePath nor model.onnx resolves.
func findFirstOnnx(dir string) (string, error) {
	var found string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || found != "" || info.IsDir() {
			return walkErr
		}
		if strings.HasSuffix(strings.ToLower(path), ".onnx") {
			found = path
		}
		return nil
	})
	return found, err
}

// Destroy releases the onnxruntime session. The environment itself is
// process-wide and is intentionally NOT destroyed here — other
// recognizers may still be using it.
func (r *GLiNERRecognizer) Destroy() error {
	if r.session != nil {
		err := r.session.Destroy()
		r.session = nil
		return err
	}
	return nil
}

// Analyze runs the GLiNER model on text and returns canonical PII
// entities. Mirrors HugotNERRecognizer's panic-recover discipline so
// one bad document fails the doc, not the whole batch.
func (r *GLiNERRecognizer) Analyze(ctx context.Context, text string, entities []string, _ string) (results []analyzer.RecognizerResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			results = nil
			err = fmt.Errorf("gliner: panic during analyze (likely upstream tokenizer/inference bug): %v", rec)
		}
	}()

	if GLiNERDebug {
		debugLog("Analyze: text.len=%d\n", len(text))
	}

	if err := r.init(ctx); err != nil {
		return nil, err
	}

	wantAll := len(entities) == 0
	want := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		want[e] = struct{}{}
	}

	chunkChars := r.cfg.ChunkChars
	if chunkChars == 0 {
		chunkChars = defaultGLiNERChunkChars
	}
	chunkOverlap := r.cfg.ChunkOverlap
	if chunkOverlap == 0 {
		chunkOverlap = defaultGLiNERChunkOverlap
	}
	chunks := chunkForNER(text, chunkChars, chunkOverlap)

	cands := make([]hugotCand, 0, len(chunks)*8)
	for _, chunk := range chunks {
		spans, runErr := r.runChunk(chunk.Text)
		if runErr != nil {
			if GLiNERDebug {
				debugLog("Analyze: chunk @%d error: %v\n", chunk.ByteStart, runErr)
			}
			return nil, fmt.Errorf("gliner: chunk @%d: %w", chunk.ByteStart, runErr)
		}
		if GLiNERDebug {
			debugLog("Analyze: chunk @%d returned %d spans\n", chunk.ByteStart, len(spans))
		}
		for _, s := range spans {
			canonical, ok := r.labelToEntity[s.label]
			if !ok {
				continue
			}
			if !wantAll {
				if _, ok := want[canonical]; !ok {
					continue
				}
			}
			cands = append(cands, hugotCand{
				start: chunk.ByteStart + s.byteStart,
				end:   chunk.ByteStart + s.byteEnd,
				score: s.score,
				typ:   canonical,
			})
		}
	}

	// Per-call summary at INFO. One line per /v1/ingest is a fine
	// volume — fly logs already get healthcheck spam at 6/min, and this
	// is the diagnostic that tells operators whether GLiNER is the cause
	// of any "PII missed" complaints. Toggle with GLINER_QUIET=1 if it
	// gets noisy under bulk traffic.
	if os.Getenv("GLINER_QUIET") != "1" {
		log.Printf("gliner: analyze text_bytes=%d chunks=%d raw_candidates=%d threshold=%.2f",
			len(text), len(chunks), len(cands), r.threshold)
	}

	if len(cands) == 0 {
		return nil, nil
	}

	// Type-grouped overlap dedup, identical strategy to
	// HugotNERRecognizer: sliding-window chunks pick the same entity
	// twice at slightly different boundaries; keep the higher-scoring
	// span per type, let inter-type overlap go to the analyzer's
	// conflict resolver.
	byType := map[string][]hugotCand{}
	for _, c := range cands {
		byType[c.typ] = append(byType[c.typ], c)
	}
	for _, group := range byType {
		sort.Slice(group, func(i, j int) bool {
			if group[i].score != group[j].score {
				return group[i].score > group[j].score
			}
			return group[i].start < group[j].start
		})
		kept := group[:0]
		for _, c := range group {
			overlapsKept := false
			for _, k := range kept {
				if c.start < k.end && c.end > k.start {
					overlapsKept = true
					break
				}
			}
			if !overlapsKept {
				kept = append(kept, c)
			}
		}
		for _, c := range kept {
			results = append(results, analyzer.RecognizerResult{
				Start:          c.start,
				End:            c.end,
				Score:          c.score,
				EntityType:     c.typ,
				RecognizerName: r.Name(),
			})
		}
	}
	return results, nil
}

// glinerSpan is a single decoded span from one chunk, with chunk-local
// byte offsets and the raw prompt label (pre canonical mapping).
type glinerSpan struct {
	byteStart int
	byteEnd   int
	label     string
	score     float64
}

// wordRange holds the byte-range of a text word in the chunk text.
type wordRange struct{ start, end int }

// runChunk encodes one chunk of text through the full prompt+model+
// decoder pipeline and returns the spans above threshold.
//
// Tokenisation strategy: gomlx/hftokenizer's `Spans` are unreliable
// inside Metaspace+Unigram (byte offsets get computed against the
// metaspace'd word text, not the original input). Instead of trusting
// `Spans`, we tokenize the prompt prefix and each text word
// INDEPENDENTLY and concatenate the IDs. Word boundaries are then
// trivially known by construction — the first subword of word_i is
// 1-indexed `i`, continuations are 0.
//
// `AddSpecialTokens` is disabled for the per-piece encodes; we add the
// `[CLS]` / `[SEP]` IDs ourselves so we can keep total control over
// the sequence layout.
//
// Unlike the previous gomlx port, onnxruntime accepts arbitrary input
// shapes per call, so we DO NOT pad to maxTokens. Each chunk's tensors
// are sized to the actual seq_len / num_words / num_spans — same shape
// regime the Python sidecar feeds the model. `text_lengths = [[numWords]]`
// (shape `[1, 1]`) matches gliner/data_processing/processor.py:530
// (`seq_length.unsqueeze(-1)`).
func (r *GLiNERRecognizer) runChunk(text string) ([]glinerSpan, error) {
	// --- 1. Word split (Python sidecar reference behaviour). ----------
	wordRanges := splitWords(text)
	if len(wordRanges) == 0 {
		return nil, nil
	}

	// --- 2. Tokenize prompt prefix + each text word independently. ----
	// We disable special-token post-processing for each piece so the
	// tokenizer emits ONLY the subword IDs of the input. [CLS]/[SEP]
	// are added by us at the boundaries.
	tokOpts := api.EncodeOptions{AddSpecialTokens: false, IncludeSpans: false}
	if err := r.tokenizer.With(tokOpts); err != nil {
		return nil, fmt.Errorf("tokenizer.With: %w", err)
	}
	defer func() {
		// Restore the default options so concurrent calls don't see a
		// stripped tokenizer. (init set AddSpecialTokens=true.)
		_ = r.tokenizer.With(api.EncodeOptions{
			AddSpecialTokens:         true,
			IncludeSpans:             true,
			IncludeSpecialTokensMask: true,
		})
	}()

	promptIDs := r.tokenizer.Encode(r.promptString)
	// Drop any trailing [SEP]/[CLS] that snuck through (shouldn't with
	// AddSpecialTokens=false, but defensive — DeBERTa templates can
	// inject them).
	promptIDs = stripBoundarySpecials(promptIDs, gliner_clsID(r.tokenizer), gliner_sepID(r.tokenizer))

	// Per-word tokenisation, preserving the word index for each subword.
	// The first subword of each word emits a 1-based mask value; all
	// continuation subwords emit 0.
	var (
		textIDs []int
		// wordOfToken[i] is the 1-based word index of token i in
		// textIDs, or 0 for continuation subwords.
		wordOfToken []int64
	)
	for wi, w := range wordRanges {
		piece := text[w.start:w.end]
		// Prepend a space so the SPM pre-tokenizer treats this as a
		// new word with a metaspace marker. Without the prefix space
		// the first word of each input gets tokenized differently
		// from words mid-sentence — that mismatch ruins recall.
		ids := r.tokenizer.Encode(" " + piece)
		ids = stripBoundarySpecials(ids, gliner_clsID(r.tokenizer), gliner_sepID(r.tokenizer))
		if len(ids) == 0 {
			continue
		}
		textIDs = append(textIDs, ids...)
		// First subword: 1-based word index. Rest: 0 (continuation).
		wordOfToken = append(wordOfToken, int64(wi+1))
		for j := 1; j < len(ids); j++ {
			wordOfToken = append(wordOfToken, 0)
		}
	}
	if len(textIDs) == 0 {
		return nil, nil
	}

	// --- 3. Concat: [CLS] + prompt + text + [SEP]. ---------------------
	clsID := gliner_clsID(r.tokenizer)
	sepID := gliner_sepID(r.tokenizer)
	finalIDs := make([]int, 0, 1+len(promptIDs)+len(textIDs)+1)
	finalIDs = append(finalIDs, clsID)
	finalIDs = append(finalIDs, promptIDs...)
	finalIDs = append(finalIDs, textIDs...)
	finalIDs = append(finalIDs, sepID)

	finalMask := make([]int64, 0, len(finalIDs))
	finalMask = append(finalMask, 0) // [CLS]
	for range promptIDs {
		finalMask = append(finalMask, 0)
	}
	finalMask = append(finalMask, wordOfToken...)
	finalMask = append(finalMask, 0) // [SEP]

	// --- 4. Truncate to maxTokens if needed. -----
	// We don't pad here — onnxruntime accepts dynamic shapes — but we
	// do honour the configured cap so we don't blow past the model's
	// position-embedding limit (DeBERTa-v3-small is 512).
	seqLen := len(finalIDs)
	if seqLen > r.maxTokens {
		seqLen = r.maxTokens
		finalIDs = finalIDs[:seqLen]
		finalMask = finalMask[:seqLen]
	}

	// Recompute numWords as the highest word index reached within the
	// (possibly truncated) sequence; if truncation cuts mid-word we
	// discard that word entirely so the model never sees a partial
	// word.
	maxWordReached := 0
	for i := 0; i < seqLen; i++ {
		if finalMask[i] > int64(maxWordReached) {
			maxWordReached = int(finalMask[i])
		}
	}
	numWords := maxWordReached
	if numWords == 0 {
		return nil, nil
	}
	if numWords > len(wordRanges) {
		numWords = len(wordRanges)
	}

	K := r.maxWidth
	numSpans := numWords * K

	if GLiNERDebug {
		debugLog("runChunk: text=%q wordRanges=%d promptIDs=%d textIDs=%d seqLen=%d numWords=%d numSpans=%d\n",
			text, len(wordRanges), len(promptIDs), len(textIDs), seqLen, numWords, numSpans)
	}

	// --- 5. Build the encoder input data slices. ----------------------
	inputIDs := make([]int64, seqLen)
	attnMask := make([]int64, seqLen)
	wordsMask := make([]int64, seqLen)
	for i := 0; i < seqLen; i++ {
		inputIDs[i] = int64(finalIDs[i])
		attnMask[i] = 1
		wordsMask[i] = finalMask[i]
	}

	// --- 6. Build span_idx / span_mask. ----------
	// span_idx[i, s*K+w, :] = (s, s+w). span_mask is true only where
	// the span fits inside the valid word region (s+w <= numWords-1).
	spanIdxData := make([]int64, numSpans*2)
	spanMaskData := make([]bool, numSpans)
	for s := 0; s < numWords; s++ {
		for w := 0; w < K; w++ {
			pos := s*K + w
			spanIdxData[pos*2] = int64(s)
			spanIdxData[pos*2+1] = int64(s + w)
			if (s + w) <= (numWords - 1) {
				spanMaskData[pos] = true
			}
		}
	}

	// text_lengths: per-batch num-words count. Python collator emits
	// shape [B, 1] via seq_length.unsqueeze(-1).
	textLengthsData := []int64{int64(numWords)}

	// --- 7. Allocate the ort.Tensor inputs. ----------------------
	inputIDsT, terr := ort.NewTensor(ort.NewShape(1, int64(seqLen)), inputIDs)
	if terr != nil {
		return nil, fmt.Errorf("gliner: new input_ids tensor: %w", terr)
	}
	defer inputIDsT.Destroy()

	attnMaskT, terr := ort.NewTensor(ort.NewShape(1, int64(seqLen)), attnMask)
	if terr != nil {
		return nil, fmt.Errorf("gliner: new attention_mask tensor: %w", terr)
	}
	defer attnMaskT.Destroy()

	wordsMaskT, terr := ort.NewTensor(ort.NewShape(1, int64(seqLen)), wordsMask)
	if terr != nil {
		return nil, fmt.Errorf("gliner: new words_mask tensor: %w", terr)
	}
	defer wordsMaskT.Destroy()

	textLengthsT, terr := ort.NewTensor(ort.NewShape(1, 1), textLengthsData)
	if terr != nil {
		return nil, fmt.Errorf("gliner: new text_lengths tensor: %w", terr)
	}
	defer textLengthsT.Destroy()

	spanIdxT, terr := ort.NewTensor(ort.NewShape(1, int64(numSpans), 2), spanIdxData)
	if terr != nil {
		return nil, fmt.Errorf("gliner: new span_idx tensor: %w", terr)
	}
	defer spanIdxT.Destroy()

	spanMaskT, terr := ort.NewTensor(ort.NewShape(1, int64(numSpans)), spanMaskData)
	if terr != nil {
		return nil, fmt.Errorf("gliner: new span_mask tensor: %w", terr)
	}
	defer spanMaskT.Destroy()

	// --- 8. Run inference. -------------------------------------------
	// Passing a nil output slot tells onnxruntime to auto-allocate the
	// output tensor based on the graph's inferred output shape. The
	// caller must Destroy() that tensor once read.
	inputs := []ort.Value{inputIDsT, attnMaskT, wordsMaskT, textLengthsT, spanIdxT, spanMaskT}
	outputs := []ort.Value{nil}

	r.mu.Lock()
	runErr := r.session.Run(inputs, outputs)
	r.mu.Unlock()
	if runErr != nil {
		return nil, fmt.Errorf("gliner inference (onnxruntime): %w", runErr)
	}
	if outputs[0] == nil {
		return nil, fmt.Errorf("gliner: session.Run returned nil output")
	}
	defer outputs[0].Destroy()

	logitsTensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("gliner: unexpected logits dtype, got %T", outputs[0])
	}
	logitsShape := logitsTensor.GetShape()
	dims := make([]int, len(logitsShape))
	for i, d := range logitsShape {
		dims[i] = int(d)
	}
	flatLogits := logitsTensor.GetData()

	if GLiNERDebug {
		var maxLogit float64 = -1e9
		for _, v := range flatLogits {
			if float64(v) > maxLogit {
				maxLogit = float64(v)
			}
		}
		debugLog("inference: logitsShape=%v max-logit=%.3f threshold=%.3f\n",
			dims, maxLogit, r.threshold)
	}

	// GLiNER span head emits [B, L, K, C]: batch, num_words, max_width,
	// num_classes. The Python decoder reads the same shape. We also
	// support a flat [B, L*K, C] layout for older exports.
	var LDim, KDim, numClasses int
	switch len(dims) {
	case 4:
		if dims[0] != 1 {
			return nil, fmt.Errorf("unexpected logits shape (4-D): %v", dims)
		}
		LDim = dims[1]
		KDim = dims[2]
		numClasses = dims[3]
	case 3:
		if dims[0] != 1 {
			return nil, fmt.Errorf("unexpected logits shape (3-D): %v", dims)
		}
		LDim = dims[1] / r.maxWidth
		KDim = r.maxWidth
		numClasses = dims[2]
	default:
		return nil, fmt.Errorf("unexpected logits shape: %v", dims)
	}
	if numClasses != len(r.labels) {
		return nil, fmt.Errorf("model returned %d classes, expected %d (labels)", numClasses, len(r.labels))
	}

	// --- 9. Decode: sigmoid > threshold, greedy non-overlap. ---------
	threshold := r.threshold
	// Pre-sigmoid threshold lets us reject without per-class exp().
	logitThresh := math.Log(threshold/(1-threshold)) - 1e-9

	type spanCand struct {
		s, w, c int
		score   float64
	}
	cands := make([]spanCand, 0, 32)
	// Iterate (s, w) — for each (start word, width) pair, scan classes.
	// The decoder's validity check (`start + width + 1 <= num_words`)
	// matches the Python reference.
	for s := 0; s < LDim; s++ {
		if s >= numWords {
			break
		}
		for w := 0; w < KDim; w++ {
			if s+w+1 > numWords {
				break
			}
			base := (s*KDim + w) * numClasses
			for c := 0; c < numClasses; c++ {
				logit := float64(flatLogits[base+c])
				if logit < logitThresh {
					continue
				}
				score := 1.0 / (1.0 + math.Exp(-logit))
				if score < threshold {
					continue
				}
				cands = append(cands, spanCand{s, w, c, score})
			}
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}

	// Greedy: sort by score desc, then start asc for stability.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if cands[i].s != cands[j].s {
			return cands[i].s < cands[j].s
		}
		return cands[i].w < cands[j].w
	})

	type keptSpan struct{ s, w int }
	kept := make([]keptSpan, 0, len(cands))
	out := make([]glinerSpan, 0, len(cands))
	for _, cd := range cands {
		end := cd.s + cd.w
		conflict := false
		for _, k := range kept {
			if cd.s <= k.s+k.w && k.s <= end {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		kept = append(kept, keptSpan{cd.s, cd.w})
		startByte := wordRanges[cd.s].start
		endByte := wordRanges[cd.s+cd.w].end
		out = append(out, glinerSpan{
			byteStart: startByte,
			byteEnd:   endByte,
			label:     r.labels[cd.c],
			score:     cd.score,
		})
	}
	return out, nil
}

// splitWords applies the GLiNER WhitespaceTokenSplitter regex to text
// and returns each word's byte-range.
func splitWords(text string) []wordRange {
	matches := glinerWordRegex.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]wordRange, len(matches))
	for i, m := range matches {
		out[i] = wordRange{start: m[0], end: m[1]}
	}
	return out
}

// gliner_clsID returns the [CLS] token ID. The DeBERTa tokenizer.json
// has [CLS] at id=1; cached after first resolution so repeated calls
// are O(1).
func gliner_clsID(_ *hftokenizer.Tokenizer) int {
	return 1
}

// gliner_sepID returns the [SEP] token ID — id=2 in DeBERTa.
func gliner_sepID(_ *hftokenizer.Tokenizer) int {
	return 2
}

// stripBoundarySpecials strips leading [CLS] and trailing [SEP] from a
// token-ID slice. Used to defend against tokenizer post-processors
// re-adding specials even when we set AddSpecialTokens=false (a known
// behaviour on some HF tokenizer setups). We add the boundary specials
// ourselves at the [CLS]/.../[SEP] concat step.
func stripBoundarySpecials(ids []int, cls, sep int) []int {
	if len(ids) == 0 {
		return ids
	}
	if ids[0] == cls {
		ids = ids[1:]
	}
	if len(ids) > 0 && ids[len(ids)-1] == sep {
		ids = ids[:len(ids)-1]
	}
	return ids
}
