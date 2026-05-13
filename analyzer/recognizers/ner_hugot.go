//go:build hugot

// Package-level build tag: this file (and the hugot transitive deps —
// onnxruntime-go, tokenizers, …) compile only when building with
// `go build -tags hugot ...`. Default builds exclude hugot entirely to
// keep compile time, binary size, and dependency surface small. The
// platform binary's "hugot" backend falls back to a fatal-error stub
// (see ../../hugot_off.go) when this file isn't compiled.

package recognizers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/backends"
	"github.com/knights-analytics/hugot/pipelines"
	"github.com/anonde-io/anonde/analyzer"
)

// Default chunk sizing for sliding-window NER. The XLM-RoBERTa default
// model has a 512-token context; ~1500 chars of German clinical text
// tokenizes to roughly 400 tokens, leaving headroom for special tokens.
// 200 chars of overlap catches entities sitting on chunk boundaries.
const (
	defaultNERChunkChars   = 1500
	defaultNERChunkOverlap = 200
)

// HugotNERConfig lives in ner_hugot_config.go (no build tag) so the
// top-level package's hugot_off.go stub can name it in a signature.

// hugotLabelToEntity maps both CoNLL-2003 labels and ai4privacy-fine-tuned
// model labels to the entity types used by anonde.
//
// Deliberate scope: we ONLY map natural-language entities (people,
// locations, organizations) from the NER model. Structured PII — emails,
// phones, IPs, MAC, IBAN, credit cards, SSN, crypto wallets — comes from
// the regex/checksum recognizers, which produce span-exact matches the
// model can't. Allowing the model to emit those labels causes overlapping
// findings with subtly wrong spans that crowd out the precise regex hits at
// conflict resolution time and tank precision.
//
// Models that emit labels outside this table contribute nothing — `ok=false`
// causes the finding to be dropped.
var hugotLabelToEntity = map[string]string{
	// CoNLL-2003 / general NER
	"PER":  "PERSON",
	"LOC":  "LOCATION",
	"ORG":  "ORGANIZATION",
	"MISC": "NRP",

	// ai4privacy: people
	"FIRSTNAME":  "PERSON",
	"LASTNAME":   "PERSON",
	"MIDDLENAME": "PERSON",
	"USERNAME":   "PERSON",
	"PREFIX":     "PERSON",

	// multilang-pii-ner labels (XLM-RoBERTa, EN/DE/IT/FR)
	"GIVENNAME": "PERSON",
	"SURNAME":   "PERSON",

	// ai4privacy / multilang-pii-ner: locations
	// STREET/BUILDINGNUM/ZIPCODE go to the address-bucket entity types so
	// they score under canonical ADDRESS in the bench, not LOCATION.
	"CITY":             "LOCATION",
	"STATE":            "LOCATION",
	"COUNTY":           "LOCATION",
	"STREET":           "STREET_ADDRESS",
	"BUILDINGNUMBER":   "STREET_ADDRESS",
	"BUILDINGNUM":      "STREET_ADDRESS",
	"ZIPCODE":          "POSTAL_CODE",
	"SECONDARYADDRESS": "STREET_ADDRESS",

	// ai4privacy: orgs
	"COMPANYNAME": "ORGANIZATION",
	"JOBTITLE":    "ORGANIZATION",
	"JOBAREA":     "ORGANIZATION",
	"JOBTYPE":     "ORGANIZATION",

	// AGE / BUILDINGNUM are deliberately NOT mapped: the multilang-pii-ner
	// model emits them too liberally on clinical text. We rely on the
	// dedicated DEAgeRecognizer (context-gated) and the existing
	// DEPostalCodeRecognizer / DEStreetRecognizer for these.
}

// defaultNERScoreFloor drops NER predictions below this score before the
// analyzer's threshold filter sees them. The XLM-R PII model is noisy on
// clinical German text — many sub-0.6 outputs are spurious. Tunable via
// HugotNERConfig.ScoreFloor (zero uses this default).
const defaultNERScoreFloor = 0.60

// MapHugotLabel converts a raw hugot label (e.g. "PER", "B-LOC", "FIRSTNAME")
// to an anonde entity type string.  Returns ("", false) when the label is
// unknown — caller drops the finding.
func MapHugotLabel(label string) (string, bool) {
	upper := strings.ToUpper(label)
	if et, ok := hugotLabelToEntity[upper]; ok {
		return et, true
	}
	// Strip B-/I- prefix for non-aggregated output (e.g. "B-PER" → "PER").
	if len(upper) > 2 && upper[1] == '-' {
		if et, ok := hugotLabelToEntity[upper[2:]]; ok {
			return et, true
		}
	}
	return "", false
}

// HugotNERRecognizer uses a pre-trained ONNX transformer model (via hugot) to
// detect named entities.  The model runs entirely in-process with no CGO or
// external service required.
//
// On the first call to Analyze the session and pipeline are initialised lazily;
// subsequent calls reuse them.  The model is downloaded automatically from
// HuggingFace Hub when AutoDownload is true and the model directory does not
// yet exist.
//
// Note: the pure-Go ONNX backend performs CPU inference; expect higher latency
// than GPU-accelerated or native-library backends, especially with large models.
type HugotNERRecognizer struct {
	cfg      HugotNERConfig
	once     sync.Once
	initErr  error
	session  *hugot.Session
	pipeline *pipelines.TokenClassificationPipeline
}

// NewHugotNERRecognizer creates a new hugot-backed NER recognizer.
// Call with a zero-value config to use all defaults.
func NewHugotNERRecognizer(cfg HugotNERConfig) *HugotNERRecognizer {
	if cfg.ModelsDir == "" {
		home, _ := os.UserHomeDir()
		cfg.ModelsDir = filepath.Join(home, ".cache", "anonde", "models")
	}
	if cfg.ModelName == "" {
		// onnx-community/multilang-pii-ner-ONNX is XLM-RoBERTa-base
		// fine-tuned for PII detection across EN/DE/IT/FR. Trained on
		// PII-labelled data (GIVENNAME, SURNAME, CITY, STREET,
		// BUILDINGNUM, ZIPCODE, AGE, …) rather than CoNLL-2003 news —
		// substantially better recall on German clinical text than the
		// previous Xenova/distilbert-multilingual default.
		cfg.ModelName = "onnx-community/multilang-pii-ner-ONNX"
		// Repos ship multiple ONNX variants; pick the int8-quantized
		// build for the production size/speed sweet spot (~280 MB).
		if cfg.OnnxFilePath == "" {
			cfg.OnnxFilePath = "onnx/model_quantized.onnx"
		}
	}
	return &HugotNERRecognizer{cfg: cfg}
}

func (r *HugotNERRecognizer) Name() string { return "HugotNERRecognizer" }
func (r *HugotNERRecognizer) SupportedEntities() []string {
	return []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"}
}
// SupportedLanguages enumerates the languages the default multilingual model
// is trained on. Callers using a different ModelName should override the
// AnalyzerEngine's language filtering accordingly. Keep this list in sync
// with the model card.
func (r *HugotNERRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt", "ar", "lv", "zh"}
}

// init lazily starts the hugot session and loads the ONNX pipeline.
func (r *HugotNERRecognizer) init(ctx context.Context) error {
	r.once.Do(func() {
		session, err := hugot.NewGoSession(ctx)
		if err != nil {
			r.initErr = fmt.Errorf("hugot: create session: %w", err)
			return
		}
		r.session = session

		// hugot.DownloadModel chooses its own on-disk directory layout
		// (replaces "/" with "_"), so we must use the path it returns
		// rather than guess one. When the model is already present we
		// reconstruct that same path locally.
		modelPath := filepath.Join(r.cfg.ModelsDir, sanitizeModelName(r.cfg.ModelName))

		if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
			if !r.cfg.AutoDownload {
				r.initErr = fmt.Errorf("hugot: model not found at %s; set AutoDownload: true or download manually", modelPath)
				return
			}
			if mkErr := os.MkdirAll(r.cfg.ModelsDir, 0o755); mkErr != nil {
				r.initErr = fmt.Errorf("hugot: create models dir: %w", mkErr)
				return
			}
			downloadOpts := hugot.NewDownloadOptions()
			if r.cfg.OnnxFilePath != "" {
				downloadOpts.OnnxFilePath = r.cfg.OnnxFilePath
			}
			downloadedPath, dlErr := hugot.DownloadModel(ctx, r.cfg.ModelName, r.cfg.ModelsDir, downloadOpts)
			if dlErr != nil {
				r.initErr = fmt.Errorf("hugot: download model %s: %w", r.cfg.ModelName, dlErr)
				return
			}
			modelPath = downloadedPath
		}

		pipeline, err := hugot.NewPipeline(session, hugot.TokenClassificationConfig{
			ModelPath: modelPath,
			Name:      "anonde-ner",
			Options: []backends.PipelineOption[*pipelines.TokenClassificationPipeline]{
				pipelines.WithSimpleAggregation(),
				pipelines.WithIgnoreLabels([]string{"O"}),
			},
		})
		if err != nil {
			r.initErr = fmt.Errorf("hugot: init pipeline: %w", err)
			return
		}
		r.pipeline = pipeline
	})
	return r.initErr
}

// Analyze runs the pre-trained NER model and returns recognised entities.
//
// Hugot's tokenizer post-processing has known panic paths on certain inputs
// (slice-bounds violations in gatherPreEntities for some text lengths). We
// recover those panics here and surface them as errors so the analyzer engine
// continues processing other recognizers and other documents instead of
// crashing the batch.
func (r *HugotNERRecognizer) Analyze(ctx context.Context, text string, entities []string, _ string) (results []analyzer.RecognizerResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			results = nil
			err = fmt.Errorf("hugot: panic during analyze (likely upstream tokenizer bug): %v", rec)
		}
	}()

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
		chunkChars = defaultNERChunkChars
	}
	chunkOverlap := r.cfg.ChunkOverlap
	if chunkOverlap == 0 {
		chunkOverlap = defaultNERChunkOverlap
	}
	chunks := chunkForNER(text, chunkChars, chunkOverlap)

	// Build the batch of inputs. Trailing-space workaround for upstream
	// hugot bug (gatherPreEntities slice-bounds panic on certain text
	// lengths) — the space doesn't shift entity offsets relative to the
	// chunk start.
	inputs := make([]string, len(chunks))
	for i, c := range chunks {
		inputs[i] = c.Text + " "
	}

	// Single batched RunPipeline call. ONNX runtime amortises kernel
	// launches and tokenizer setup across all chunks in the batch, which
	// is dramatically cheaper than N separate calls on CPU.
	output, runErr := r.pipeline.RunPipeline(ctx, inputs)
	if runErr != nil {
		return nil, fmt.Errorf("hugot: run pipeline (%d chunks): %w", len(chunks), runErr)
	}
	if len(output.Entities) == 0 {
		return nil, nil
	}

	scoreFloor := r.cfg.ScoreFloor
	switch {
	case scoreFloor < 0: // negative = disabled
		scoreFloor = -1
	case scoreFloor == 0:
		scoreFloor = defaultNERScoreFloor
	}

	// We collect candidates first, then dedupe by overlap (not just exact
	// match). With overlapping chunks the same entity is often picked up
	// twice at slightly different boundaries — keep the higher-scoring
	// span and drop the smaller overlap.
	cands := make([]hugotCand, 0, len(chunks)*8)

	// output.Entities[i] corresponds to inputs[i] / chunks[i].
	for i, chunk := range chunks {
		if i >= len(output.Entities) {
			break
		}
		// Hugot's tokenizer returns codepoint (rune) offsets RELATIVE
		// to the chunk text. Convert to chunk-byte offsets, then add
		// the chunk's byte start in the original to get the global
		// byte offset the rest of anonde expects.
		chunkR2B := buildRuneToByteIndex(chunk.Text)
		for _, ent := range output.Entities[i] {
			entityType, ok := MapHugotLabel(ent.Entity)
			if !ok {
				continue
			}
			if !wantAll {
				if _, ok := want[entityType]; !ok {
					continue
				}
			}

			score := float64(ent.Score)
			if score < scoreFloor {
				continue
			}

			startChunk := runeOffsetToByte(chunkR2B, int(ent.Start))
			endChunk := runeOffsetToByte(chunkR2B, int(ent.End))
			startByte := chunk.ByteStart + startChunk
			endByte := chunk.ByteStart + endChunk

			cands = append(cands, hugotCand{startByte, endByte, score, entityType})
		}
	}

	// Overlap dedup: for each (type), keep the highest-score span among
	// any group that mutually overlaps. Two same-type overlapping
	// detections almost always refer to the same entity; the higher score
	// is the better-anchored span. Inter-type overlaps are passed through
	// — the analyzer's conflict resolver decides between them.
	byType := map[string][]hugotCand{}
	for _, c := range cands {
		byType[c.typ] = append(byType[c.typ], c)
	}
	for typ, group := range byType {
		// Sort by score desc, then by start asc for stability.
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
		// Adjacent same-type spans are intentionally NOT merged here.
		// Many PII corpora (ai4privacy/pii-masking-200k included) annotate
		// each name component as a separate span — "John Smith" is two
		// gold PERSON entries, "Dr. Feeney" is two PERSON entries, etc.
		// Merging at the recognizer level breaks span-exact comparison
		// against those corpora (~25% of docs in ai4privacy contain at
		// least one adjacent pair, dropping PERSON F1 from 0.83 to 0.63).
		// The anonymizer applies an adjacency merge at tokenization time
		// instead — see anonymizer.Anonymize — so user-facing output
		// produces one <PERSON> token per name even though the analyzer
		// emits two.
		for _, c := range kept {
			results = append(results, analyzer.RecognizerResult{
				Start:          c.start,
				End:            c.end,
				Score:          c.score,
				EntityType:     typ,
				RecognizerName: r.Name(),
			})
		}
		_ = typ
	}
	return results, nil
}

// hugotCand is an internal candidate span produced by the model before
// type-grouped overlap dedup.
type hugotCand struct {
	start, end int
	score      float64
	typ        string
}

// nerChunk is a slice of input text fed to the model. ByteStart is its
// offset in the original document so model-relative entity positions can
// be lifted back to global offsets.
type nerChunk struct {
	ByteStart int
	Text      string
}

// chunkForNER splits text into chunks of at most chunkChars bytes, breaking
// on the latest newline-or-space before the limit. Adjacent chunks overlap
// by overlapChars to catch entities sitting near chunk boundaries.
//
// All cuts are made at ASCII whitespace bytes (' ' or '\n'), which are
// always at rune boundaries — so the returned chunks are valid UTF-8 even
// when the input contains multi-byte runes like ä/ö/ü/ß.
//
// If text is short enough to fit in one chunk, returns a single chunk
// containing the whole text (and the recognizer behaves identically to a
// non-chunking implementation).
func chunkForNER(text string, chunkChars, overlapChars int) []nerChunk {
	n := len(text)
	if n <= chunkChars || chunkChars <= 0 {
		return []nerChunk{{ByteStart: 0, Text: text}}
	}
	if overlapChars < 0 || overlapChars >= chunkChars {
		overlapChars = 0
	}

	var out []nerChunk
	start := 0
	for start < n {
		end := start + chunkChars
		if end >= n {
			out = append(out, nerChunk{ByteStart: start, Text: text[start:]})
			break
		}
		// Prefer the last newline, fall back to the last space within
		// the window. Both are single-byte ASCII so end+1 is always a
		// rune boundary.
		if idx := strings.LastIndex(text[start:end], "\n"); idx > 0 {
			end = start + idx + 1
		} else if idx := strings.LastIndex(text[start:end], " "); idx > 0 {
			end = start + idx + 1
		} else {
			// No whitespace anywhere in this window — exotic in
			// natural text. Back up to a rune boundary so the slice
			// stays valid UTF-8.
			for end > start && (text[end]&0xC0) == 0x80 {
				end--
			}
		}
		out = append(out, nerChunk{ByteStart: start, Text: text[start:end]})

		// Step forward by (chunkChars - overlap), then snap to the
		// next whitespace for rune-safety and so we don't begin a
		// chunk mid-word.
		// No useful overlap if the stride would land past chunk end.
		nextStart := min(start+chunkChars-overlapChars, end)
		for nextStart < n && text[nextStart] != ' ' && text[nextStart] != '\n' {
			nextStart++
		}
		for nextStart < n && (text[nextStart] == ' ' || text[nextStart] == '\n') {
			nextStart++
		}
		if nextStart <= start {
			nextStart = end // ensure forward progress
		}
		start = nextStart
	}
	return out
}

// buildRuneToByteIndex returns a slice where index[i] is the byte offset of
// the i-th rune in text. The final entry equals len(text). One pass, O(n).
func buildRuneToByteIndex(text string) []int {
	idx := make([]int, 0, len(text)+1)
	for bi := range text { // rune-iteration: bi is byte index of each rune
		idx = append(idx, bi)
	}
	idx = append(idx, len(text))
	return idx
}

func runeOffsetToByte(r2b []int, runeOff int) int {
	if runeOff < 0 {
		return 0
	}
	if runeOff >= len(r2b) {
		return r2b[len(r2b)-1]
	}
	return r2b[runeOff]
}

// Destroy releases the hugot session and any underlying resources.
// Call this when the recognizer is no longer needed.
func (r *HugotNERRecognizer) Destroy() error {
	if r.session != nil {
		return r.session.Destroy()
	}
	return nil
}

// sanitizeModelName converts a HuggingFace model ID (e.g. "dslim/bert-base-NER")
// to the local directory name hugot.DownloadModel uses internally — slashes
// become underscores. Keep this in lock-step with hugot's downloader; if the
// upstream convention changes, prefer the path returned by DownloadModel.
func sanitizeModelName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
