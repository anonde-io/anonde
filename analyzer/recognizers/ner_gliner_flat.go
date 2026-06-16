//go:build ner

// ner_gliner_flat.go runs the flat (token / sequence-tag) decoder GLiNER
// variants in-process. Companion to ner_gliner.go (the uni-encoder SPAN
// decoder used by gliner-pii-base-v1.0).
//
// Flat-decoder exports (e.g. gliner-pii-large-v1.0) take 4 ONNX inputs
// (input_ids, attention_mask, words_mask, text_lengths) instead of 6 — no
// span_idx/span_mask, the model emits per-word BIO logits directly. The
// output `logits` is shape [B, L, C, 3], the trailing 3-vector per
// (word, class) being [start, end, inside]. Reference:
// gliner/decoding/decoder.py::TokenDecoder.decode. Per-chunk decoding is
// start/end pair assembly with an inside gate; score = min(start_sig,
// end_sig, inside_min), matching gliner-py's combined.min() rule.
//
// Everything else (tokenizer setup, per-piece prompt encode, chunking,
// max-chunk cap, recover discipline, per-class threshold table) is
// identical to ner_gliner.go. Compiled only under `-tags ner` AND
// CGO_ENABLED=1; the no-tag build uses ner_gliner_flat_off.go.

package recognizers

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
	"github.com/knights-analytics/hugot"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
)

// GLiNERFlatRecognizer runs a flat-decoder GLiNER ONNX export to detect
// PII entities. Open-set NER architecture; the label set is supplied
// at inference time via the prompt, same as GLiNERRecognizer.
//
// Naming: ends in "NERRecognizer" so the analyzer engine's DisableNER
// flag silences it alongside GLiNERRecognizer. "Flat" distinguishes
// from the span variant.
type GLiNERFlatRecognizer struct {
	cfg GLiNERConfig

	once    sync.Once
	initErr error

	tokenizer *hftokenizer.Tokenizer

	// onnxruntime session for this recognizer's (model, label-set).
	// Serialised by `mu`; see ner_gliner.go for the rationale.
	session *ort.DynamicAdvancedSession
	mu      sync.Mutex

	// ONNX I/O names captured at init time. Useful for diagnostics.
	onnxInputNames []string
	onnxOutputName string

	modelPath string

	// Resolved at init.
	labels        []string
	labelToEntity map[string]string
	threshold     float64
	maxWidth      int
	maxTokens     int

	// Pre-computed prompt-prefix metadata.
	promptString     string
	promptCharLength int

	// promptIDs caches the tokenised prompt prefix (post strip-specials).
	// See ner_gliner.go for the per-piece encode rationale (Lever 0).
	promptIDs []int
}

// NewGLiNERFlatRecognizer constructs a recognizer with the given config.
// A zero-value config selects knowledgator/gliner-pii-large-v1.0 with the
// default PII label set.
//
// The caller is responsible for setting OnnxFilePath to match the model
// repo's on-disk layout. For the LARGE export the file lives at
// `model.onnx` (no `onnx/` subdir, unlike the base).
func NewGLiNERFlatRecognizer(cfg GLiNERConfig) *GLiNERFlatRecognizer {
	if cfg.ModelsDir == "" {
		home, _ := os.UserHomeDir()
		cfg.ModelsDir = filepath.Join(home, ".cache", "anonde", "models")
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "knowledgator/gliner-pii-large-v1.0"
		if cfg.OnnxFilePath == "" {
			cfg.OnnxFilePath = "model.onnx"
		}
	}
	return &GLiNERFlatRecognizer{cfg: cfg}
}

// Name returns the recognizer name. MUST end in "NERRecognizer" so the
// DisableNER suffix-check fires.
func (r *GLiNERFlatRecognizer) Name() string { return "GLiNERFlatNERRecognizer" }

// SupportedEntities returns the deduplicated set of canonical entity
// types reachable via the label→entity map.
func (r *GLiNERFlatRecognizer) SupportedEntities() []string {
	m := r.cfg.LabelToEntity
	if len(m) == 0 {
		m = defaultGLiNERLabelToEntity()
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

// SupportedLanguages: GLiNER models are typically multilingual.
func (r *GLiNERFlatRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt"}
}

// init runs the one-time setup: locate model files, build the static
// label prompt, initialise the shared onnxruntime environment, and open
// a DynamicAdvancedSession over the ONNX file.
//
// Mirrors GLiNERRecognizer.init except for the 4-input ONNX I/O signature.
func (r *GLiNERFlatRecognizer) init(ctx context.Context) error {
	r.once.Do(func() {
		// Catch panics so a partially-initialised recognizer becomes
		// a clean error rather than a never-clearing panic loop,
		// same rationale as GLiNERRecognizer.init().
		defer func() {
			if rec := recover(); rec != nil {
				r.initErr = fmt.Errorf("gliner-flat init panicked: %v", rec)
				log.Printf("gliner-flat: INIT PANIC: %v", rec)
			}
		}()
		// --- config defaults --------------------------------------
		r.labels = r.cfg.Labels
		if len(r.labels) == 0 {
			r.labels = defaultGLiNERLabels()
		}
		r.labelToEntity = r.cfg.LabelToEntity
		if len(r.labelToEntity) == 0 {
			r.labelToEntity = defaultGLiNERLabelToEntity()
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
		log.Printf("gliner-flat: init starting model=%s onnx_file=%s models_dir=%s labels=%d threshold=%.2f auto_download=%v",
			r.cfg.ModelName, r.cfg.OnnxFilePath, r.cfg.ModelsDir,
			len(r.labels), r.threshold, r.cfg.AutoDownload)

		modelPath := filepath.Join(r.cfg.ModelsDir, sanitizeModelName(r.cfg.ModelName))
		if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
			if !r.cfg.AutoDownload {
				r.initErr = fmt.Errorf("gliner-flat: model not found at %s; set AutoDownload: true or download manually", modelPath)
				log.Printf("gliner-flat: INIT FAILED: %v", r.initErr)
				return
			}
			if mkErr := os.MkdirAll(r.cfg.ModelsDir, 0o755); mkErr != nil {
				r.initErr = fmt.Errorf("gliner-flat: create models dir: %w", mkErr)
				log.Printf("gliner-flat: INIT FAILED: %v", r.initErr)
				return
			}
			log.Printf("gliner-flat: downloading %s into %s (onnx=%s)", r.cfg.ModelName, r.cfg.ModelsDir, r.cfg.OnnxFilePath)
			downloadOpts := hugot.NewDownloadOptions()
			if r.cfg.OnnxFilePath != "" {
				downloadOpts.OnnxFilePath = r.cfg.OnnxFilePath
			}
			downloadedPath, dlErr := hugot.DownloadModel(ctx, r.cfg.ModelName, r.cfg.ModelsDir, downloadOpts)
			if dlErr != nil {
				r.initErr = fmt.Errorf("gliner-flat: download model %s: %w", r.cfg.ModelName, dlErr)
				log.Printf("gliner-flat: INIT FAILED: %v", r.initErr)
				return
			}
			modelPath = downloadedPath
		}
		r.modelPath = modelPath

		// --- tokenizer --------------------------------------------
		tokPath := filepath.Join(modelPath, "tokenizer.json")
		if _, err := os.Stat(tokPath); err != nil {
			r.initErr = fmt.Errorf("gliner-flat: tokenizer.json missing under %s: %w", modelPath, err)
			log.Printf("gliner-flat: INIT FAILED: %v", r.initErr)
			return
		}
		tok, err := hftokenizer.NewFromFile(nil, tokPath)
		if err != nil {
			r.initErr = fmt.Errorf("gliner-flat: load tokenizer: %w", err)
			log.Printf("gliner-flat: INIT FAILED: %v", r.initErr)
			return
		}
		if err := tok.With(api.EncodeOptions{
			AddSpecialTokens:         true,
			IncludeSpans:             true,
			IncludeSpecialTokensMask: true,
		}); err != nil {
			r.initErr = fmt.Errorf("gliner-flat: configure tokenizer: %w", err)
			return
		}
		r.tokenizer = tok

		// Pre-build the prompt prefix string:
		//   "<<ENT>> label1 <<ENT>> label2 ... <<SEP>> "
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

		// Per-piece prompt encoding. See ner_gliner.go for the
		// metaspace-bug rationale; must NOT regress.
		if err := tok.With(api.EncodeOptions{AddSpecialTokens: false, IncludeSpans: false}); err != nil {
			r.initErr = fmt.Errorf("gliner-flat: configure tokenizer for prompt encode: %w", err)
			return
		}
		clsID := gliner_clsID(tok)
		sepID := gliner_sepID(tok)
		encodePiece := func(s string) []int {
			return stripBoundarySpecials(tok.Encode(s), clsID, sepID)
		}
		var promptIDs []int
		for _, lbl := range r.labels {
			// <<ENT>> as an atomic added token, no leading space.
			promptIDs = append(promptIDs, encodePiece(gliner_entToken)...)
			// " "+label forces SPM to emit ONE leading metaspace marker.
			promptIDs = append(promptIDs, encodePiece(" "+lbl)...)
		}
		promptIDs = append(promptIDs, encodePiece(gliner_sepToken)...)
		r.promptIDs = promptIDs
		if err := tok.With(api.EncodeOptions{
			AddSpecialTokens:         true,
			IncludeSpans:             true,
			IncludeSpecialTokensMask: true,
		}); err != nil {
			r.initErr = fmt.Errorf("gliner-flat: restore tokenizer options after prompt encode: %w", err)
			return
		}

		// --- onnxruntime environment ------------------------------
		// Shares the process-wide ort env with GLiNERRecognizer; the
		// glinerOrtOnce guard inside initOrtEnvironment makes the
		// SetSharedLibraryPath / InitializeEnvironment pair idempotent.
		if err := initOrtEnvironment(r.cfg.SharedLibraryPath); err != nil {
			r.initErr = err
			return
		}

		// --- locate the ONNX file ---------------------------------
		// LARGE model layout has model.onnx at the repo root (no onnx/
		// subdir). Caller-supplied OnnxFilePath wins; fall back to
		// model.onnx, then any *.onnx we can find.
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
				r.initErr = fmt.Errorf("gliner-flat: no ONNX file found under %s (set OnnxFilePath)", modelPath)
				return
			}
			onnxFile = candidate
		}

		// --- open the session -------------------------------------
		// Flat-decoder input list: 4 names, no span tensors. Output
		// is "logits" with shape [2, B, L, C] (start/end stack).
		r.onnxInputNames = []string{
			"input_ids",
			"attention_mask",
			"words_mask",
			"text_lengths",
		}
		r.onnxOutputName = "logits"

		// Optional session tuning from ANONDE_ORT_* env vars. nil
		// preserves ORT defaults (intra=num cores, inter=1, graph=basic).
		// MUST be called AFTER initOrtEnvironment; NewSessionOptions
		// requires IsInitialized() == true.
		sessionOpts, optsErr := sessionOptionsFromEnv()
		if optsErr != nil {
			log.Printf("gliner-flat: sessionOptionsFromEnv: %v (falling through to ORT defaults)", optsErr)
			sessionOpts = nil
		}
		session, sessErr := ort.NewDynamicAdvancedSession(
			onnxFile,
			r.onnxInputNames,
			[]string{r.onnxOutputName},
			sessionOpts,
		)
		if sessErr != nil {
			r.initErr = fmt.Errorf("gliner-flat: open onnx session %s: %w", onnxFile, sessErr)
			log.Printf("gliner-flat: INIT FAILED: %v", r.initErr)
			return
		}
		r.session = session
		log.Printf("gliner-flat: ready model_path=%s onnx=%s tokens_max=%d width_max=%d",
			modelPath, onnxFile, r.maxTokens, r.maxWidth)
	})
	return r.initErr
}

// Destroy releases the onnxruntime session. The environment itself is
// process-wide and is intentionally NOT destroyed here.
func (r *GLiNERFlatRecognizer) Destroy() error {
	if r.session != nil {
		err := r.session.Destroy()
		r.session = nil
		return err
	}
	return nil
}

// Analyze runs the flat-decoder GLiNER model on text and returns
// canonical PII entities. Mirrors GLiNERRecognizer.Analyze including the
// MergeAdjacentSameType pass at the end.
func (r *GLiNERFlatRecognizer) Analyze(ctx context.Context, text string, entities []string, _ string) (results []analyzer.RecognizerResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			results = nil
			err = fmt.Errorf("gliner-flat: panic during analyze (likely upstream tokenizer/inference bug): %v", rec)
		}
	}()

	if GLiNERDebug {
		debugLog("FlatAnalyze: text.len=%d\n", len(text))
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

	maxChunks := r.cfg.MaxChunks
	if maxChunks == 0 {
		maxChunks = defaultGLiNERMaxChunks
	}
	if maxChunks > 0 && len(chunks) > maxChunks {
		droppedChunks := len(chunks) - maxChunks
		droppedBytes := 0
		for _, c := range chunks[maxChunks:] {
			droppedBytes += len(c.Text)
		}
		log.Printf("gliner-flat: doc exceeds max_chunks=%d (text_bytes=%d total_chunks=%d); "+
			"dropping last %d chunks (%d bytes uncovered by NER, patterns still run on full doc)",
			maxChunks, len(text), len(chunks), droppedChunks, droppedBytes)
		chunks = chunks[:maxChunks]
	}

	cands := make([]nerCand, 0, len(chunks)*8)
	for _, chunk := range chunks {
		spans, runErr := r.runChunk(chunk.Text)
		if runErr != nil {
			if GLiNERDebug {
				debugLog("FlatAnalyze: chunk @%d error: %v\n", chunk.ByteStart, runErr)
			}
			return nil, fmt.Errorf("gliner-flat: chunk @%d: %w", chunk.ByteStart, runErr)
		}
		if GLiNERDebug {
			debugLog("FlatAnalyze: chunk @%d returned %d spans\n", chunk.ByteStart, len(spans))
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
			absStart := chunk.ByteStart + s.byteStart
			absEnd := chunk.ByteStart + s.byteEnd
			// Structural-shape post-filter: drop fuzzy-type spans whose
			// surface is structurally non-PII. No-op unless Enabled.
			if r.cfg.SpanFilter.Enabled && absStart >= 0 && absEnd <= len(text) && absStart < absEnd {
				if r.cfg.SpanFilter.rejectSpanSurface(canonical, text[absStart:absEnd]) {
					if GLiNERDebug {
						debugLog("FlatAnalyze: shape-filter dropped %s %q\n", canonical, text[absStart:absEnd])
					}
					continue
				}
			}
			cands = append(cands, nerCand{
				start: absStart,
				end:   absEnd,
				score: s.score,
				typ:   canonical,
			})
		}
	}

	if os.Getenv("GLINER_QUIET") != "1" {
		log.Printf("gliner-flat: analyze text_bytes=%d chunks=%d raw_candidates=%d threshold=%.2f",
			len(text), len(chunks), len(cands), r.threshold)
	}

	if len(cands) == 0 {
		return nil, nil
	}

	// Type-grouped overlap dedup, identical strategy to GLiNERRecognizer.
	byType := map[string][]nerCand{}
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

	// Same merge step as the span recognizer; see ner_gliner.go for the
	// rationale (bench scores per-cell JSONLs emitted directly by the
	// recognizer, not after the anonymizer's downstream merge).
	results = anonymizer.MergeAdjacentSameType(results, text)
	return results, nil
}

// runChunk encodes one chunk of text through the full prompt+model+
// decoder pipeline and returns the spans above per-class threshold.
//
// Tokenisation is identical to GLiNERRecognizer.runChunk (per-piece
// encode, [CLS] + prompt + per-word subwords + [SEP], words_mask is the
// 1-based first-subword-of-word marker). Only the tensor list (4 inputs,
// no spans) and the decode loop (start/end logits at logits[0/1, 0, :, :])
// differ.
func (r *GLiNERFlatRecognizer) runChunk(text string) ([]glinerSpan, error) {
	// --- 1. Word split. -----------------------------------------------
	wordRanges := splitWords(text)
	if len(wordRanges) == 0 {
		return nil, nil
	}

	// Hold the recognizer mutex across the entire chunk. Same reasons as
	// GLiNERRecognizer.runChunk: tokenizer With() mutates global encode
	// options and DynamicAdvancedSession caches input/output slot pointers.
	r.mu.Lock()
	defer r.mu.Unlock()

	// --- 2. Tokenize per-word independently. --------------------------
	tokOpts := api.EncodeOptions{AddSpecialTokens: false, IncludeSpans: false}
	if err := r.tokenizer.With(tokOpts); err != nil {
		return nil, fmt.Errorf("tokenizer.With: %w", err)
	}
	defer func() {
		_ = r.tokenizer.With(api.EncodeOptions{
			AddSpecialTokens:         true,
			IncludeSpans:             true,
			IncludeSpecialTokensMask: true,
		})
	}()

	promptIDs := r.promptIDs

	var (
		textIDs     []int
		wordOfToken []int64
	)
	for wi, w := range wordRanges {
		piece := text[w.start:w.end]
		ids := r.tokenizer.Encode(" " + piece)
		ids = stripBoundarySpecials(ids, gliner_clsID(r.tokenizer), gliner_sepID(r.tokenizer))
		if len(ids) == 0 {
			continue
		}
		textIDs = append(textIDs, ids...)
		wordOfToken = append(wordOfToken, int64(wi+1))
		for j := 1; j < len(ids); j++ {
			wordOfToken = append(wordOfToken, 0)
		}
	}
	if len(textIDs) == 0 {
		return nil, nil
	}

	// --- 3. Concat: [CLS] + prompt + text + [SEP]. --------------------
	clsID := gliner_clsID(r.tokenizer)
	sepID := gliner_sepID(r.tokenizer)
	finalIDs := make([]int, 0, 1+len(promptIDs)+len(textIDs)+1)
	finalIDs = append(finalIDs, clsID)
	finalIDs = append(finalIDs, promptIDs...)
	finalIDs = append(finalIDs, textIDs...)
	finalIDs = append(finalIDs, sepID)

	finalMask := make([]int64, 0, len(finalIDs))
	finalMask = append(finalMask, 0)
	for range promptIDs {
		finalMask = append(finalMask, 0)
	}
	finalMask = append(finalMask, wordOfToken...)
	finalMask = append(finalMask, 0)

	// --- 4. Truncate to maxTokens if needed. --------------------------
	seqLen := len(finalIDs)
	if seqLen > r.maxTokens {
		seqLen = r.maxTokens
		finalIDs = finalIDs[:seqLen]
		finalMask = finalMask[:seqLen]
	}

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

	if GLiNERDebug {
		debugLog("FlatRunChunk: text=%q wordRanges=%d promptIDs=%d textIDs=%d seqLen=%d numWords=%d\n",
			text, len(wordRanges), len(promptIDs), len(textIDs), seqLen, numWords)
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

	// text_lengths: per-batch num-words count. Shape [B, 1],
	// gliner/data_processing/processor.py emits seq_length.unsqueeze(-1).
	textLengthsData := []int64{int64(numWords)}

	// --- 6. Allocate the ort.Tensor inputs (4 total). -----------------
	inputIDsT, terr := ort.NewTensor(ort.NewShape(1, int64(seqLen)), inputIDs)
	if terr != nil {
		return nil, fmt.Errorf("gliner-flat: new input_ids tensor: %w", terr)
	}
	defer inputIDsT.Destroy()

	attnMaskT, terr := ort.NewTensor(ort.NewShape(1, int64(seqLen)), attnMask)
	if terr != nil {
		return nil, fmt.Errorf("gliner-flat: new attention_mask tensor: %w", terr)
	}
	defer attnMaskT.Destroy()

	wordsMaskT, terr := ort.NewTensor(ort.NewShape(1, int64(seqLen)), wordsMask)
	if terr != nil {
		return nil, fmt.Errorf("gliner-flat: new words_mask tensor: %w", terr)
	}
	defer wordsMaskT.Destroy()

	textLengthsT, terr := ort.NewTensor(ort.NewShape(1, 1), textLengthsData)
	if terr != nil {
		return nil, fmt.Errorf("gliner-flat: new text_lengths tensor: %w", terr)
	}
	defer textLengthsT.Destroy()

	// --- 7. Run inference. --------------------------------------------
	inputs := []ort.Value{inputIDsT, attnMaskT, wordsMaskT, textLengthsT}
	outputs := []ort.Value{nil}

	runErr := r.session.Run(inputs, outputs)
	if runErr != nil {
		return nil, fmt.Errorf("gliner-flat inference (onnxruntime): %w", runErr)
	}
	if outputs[0] == nil {
		return nil, fmt.Errorf("gliner-flat: session.Run returned nil output")
	}
	defer outputs[0].Destroy()

	logitsTensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("gliner-flat: unexpected logits dtype, got %T", outputs[0])
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
		debugLog("FlatInference: logitsShape=%v max-logit=%.3f threshold=%.3f\n",
			dims, maxLogit, r.threshold)
	}

	// Flat decoder: logits shape [B, L, C, 3]
	//   trailing 3-vector per (word, class) = [start, end, inside] (BIO).
	// Reference: gliner/decoding/decoder.py::TokenDecoder.decode (the
	// `model_output.permute(3, 0, 1, 2)` line is the canonical split).
	// We validate the trailing 3 dimension explicitly so a span-decoder
	// export accidentally pointed at this recognizer fails loudly.
	if len(dims) != 4 {
		return nil, fmt.Errorf("gliner-flat: unexpected logits shape: %v (want 4-D [B, L, C, 3])", dims)
	}
	if dims[3] != 3 {
		return nil, fmt.Errorf("gliner-flat: logits trailing dim = %d, want 3 (start/end/inside BIO stack); is this a span-decoder export?", dims[3])
	}
	if dims[0] != 1 {
		return nil, fmt.Errorf("gliner-flat: logits batch dim = %d, want 1", dims[0])
	}
	LDim := dims[1]
	numClasses := dims[2]
	if numClasses != len(r.labels) {
		return nil, fmt.Errorf("gliner-flat: model returned %d classes, expected %d (labels)", numClasses, len(r.labels))
	}
	if LDim < numWords {
		// Defensive; model should always emit one row per word in
		// words_mask, but truncation can leave LDim < numWords if the
		// graph's L axis tracks the dense words tensor. Use whatever
		// is smaller.
		numWords = LDim
	}

	// Stride helpers. Row-major [B=1, L, C, 3]:
	//   flatLogits[((0*L + w)*C + c)*3 + k] = logit at (word w, class c, channel k)
	//   k=0 -> start, k=1 -> end, k=2 -> inside
	const (
		chStart  = 0
		chEnd    = 1
		chInside = 2
	)
	logitAt := func(w, c, k int) float64 {
		return float64(flatLogits[(w*numClasses+c)*3+k])
	}

	// Per-class thresholds. Same table as the span decoder so the bench
	// is apples-to-apples; PERSON/ORG/AGE/ID get more permissive floors.
	entityTypeThreshold := map[string]float64{
		"PERSON":       defaultPersonThreshold,
		"ORGANIZATION": defaultOrgThreshold,
		"AGE":          defaultAgeThreshold,
		"ID":           defaultIdThreshold,
	}
	perClassThresh := make([]float64, numClasses)
	perClassLogit := make([]float64, numClasses)
	for c := 0; c < numClasses; c++ {
		t := r.threshold
		if c < len(r.labels) {
			canonical := r.labelToEntity[r.labels[c]]
			// ClassThresholds is used DIRECTLY (not min()'d), so a deploy
			// can RAISE a floor above its default to cut common-word FPs.
			// Absent classes fall back to min(threshold, floor).
			if override, ok := r.cfg.ClassThresholds[canonical]; ok && override > 0 {
				t = override
			} else if floor, ok := entityTypeThreshold[canonical]; ok && t > floor {
				t = floor
			}
		}
		perClassThresh[c] = t
		perClassLogit[c] = math.Log(t/(1-t)) - 1e-9
	}

	// Pre-compute sigmoid of all three channels per (word, class) once.
	// Cheap (numWords * numClasses * 3 floats) and lets the inside-gate
	// scan avoid recomputing sigmoid for each candidate span.
	sig := func(x float64) float64 { return 1.0 / (1.0 + math.Exp(-x)) }
	sigStart := make([]float64, numWords*numClasses)
	sigEnd := make([]float64, numWords*numClasses)
	sigInside := make([]float64, numWords*numClasses)
	for w := 0; w < numWords; w++ {
		for c := 0; c < numClasses; c++ {
			idx := w*numClasses + c
			sigStart[idx] = sig(logitAt(w, c, chStart))
			sigEnd[idx] = sig(logitAt(w, c, chEnd))
			sigInside[idx] = sig(logitAt(w, c, chInside))
		}
	}

	type spanCand struct {
		s, e, c int
		score   float64
	}
	cands := make([]spanCand, 0, 32)
	// For each class:
	//   * iterate candidate start words (start sig > thresh)
	//   * for each (s,c) walk e from s while the inside gate at e holds
	//     (continuous BIO inside run); whenever end sig > thresh emit
	//     the (s,e,c) candidate with combined.min() scoring.
	// This matches gliner-py's _calculate_span_score: a candidate pair
	// is only valid if EVERY token in [s, e] is "inside", and the span
	// score is min(start, end, inside_min).
	for c := 0; c < numClasses; c++ {
		thresh := perClassThresh[c]
		preLogit := perClassLogit[c]
		for s := 0; s < numWords; s++ {
			// Cheap pre-sigmoid reject on the start channel.
			if logitAt(s, c, chStart) < preLogit {
				continue
			}
			ss := sigStart[s*numClasses+c]
			if ss < thresh {
				continue
			}
			// Walk end positions in [s, s+maxWidth) while the inside
			// gate holds at every step. The moment an inside falls
			// below threshold, every wider e is invalid; break.
			endLimit := s + r.maxWidth
			if endLimit > numWords {
				endLimit = numWords
			}
			minInside := math.Inf(1)
			for e := s; e < endLimit; e++ {
				// Inside gate for word e under class c.
				ins := sigInside[e*numClasses+c]
				if ins < thresh {
					break
				}
				if ins < minInside {
					minInside = ins
				}
				// End gate for closing the span at e.
				if logitAt(e, c, chEnd) < preLogit {
					continue
				}
				se := sigEnd[e*numClasses+c]
				if se < thresh {
					continue
				}
				// combined.min() over (start, end, all-inside),
				// matches gliner-py's scoring rule verbatim.
				score := ss
				if se < score {
					score = se
				}
				if minInside < score {
					score = minInside
				}
				cands = append(cands, spanCand{s: s, e: e, c: c, score: score})
			}
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}

	// Greedy non-overlap dedup. Mirrors the span decoder including the
	// PERSON wider-span tiebreak; see ner_gliner.go for the rationale
	// (surname-leak regression). Here the span's "width" is `e - s`.
	isPerson := func(c int) bool {
		if c < 0 || c >= len(r.labels) {
			return false
		}
		return r.labelToEntity[r.labels[c]] == "PERSON"
	}
	width := func(c spanCand) int { return c.e - c.s }
	sort.Slice(cands, func(i, j int) bool {
		if isPerson(cands[i].c) && isPerson(cands[j].c) {
			wi, wj := width(cands[i]), width(cands[j])
			if wi != wj {
				return wi > wj
			}
		}
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if cands[i].s != cands[j].s {
			return cands[i].s < cands[j].s
		}
		return cands[i].e < cands[j].e
	})

	type keptSpan struct{ s, e int }
	kept := make([]keptSpan, 0, len(cands))
	out := make([]glinerSpan, 0, len(cands))
	for _, cd := range cands {
		conflict := false
		for _, k := range kept {
			// Inclusive overlap test; same shape as the span decoder.
			if cd.s <= k.e && k.s <= cd.e {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		kept = append(kept, keptSpan{cd.s, cd.e})
		startWord := cd.s
		// Left-boundary trim: GLiNER sometimes glues a leading title /
		// role common noun ("Customer", "Mr", …) into the FRONT of a
		// PERSON span ("Customer john doe" @ 1.000). A score threshold
		// can't fix a 1.0 span, so strip the leading non-name token when
		// the remainder is still a plausible name. Conservative — never
		// truncates real multi-token names (see trimPersonLeadingNonName).
		if isPerson(cd.c) {
			if trimmed, ok := trimPersonLeadingNonName(text, wordRanges, cd.s, cd.e); ok {
				if GLiNERDebug {
					debugLog("FlatRunChunk: trimmed PERSON lead %q -> %q\n",
						text[wordRanges[cd.s].start:wordRanges[cd.e].end],
						text[wordRanges[trimmed].start:wordRanges[cd.e].end])
				}
				startWord = trimmed
			}
		}
		startByte := wordRanges[startWord].start
		endByte := wordRanges[cd.e].end
		out = append(out, glinerSpan{
			byteStart: startByte,
			byteEnd:   endByte,
			label:     r.labels[cd.c],
			score:     cd.score,
		})
	}
	return out, nil
}
