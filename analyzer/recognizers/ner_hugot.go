package recognizers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/backends"
	"github.com/knights-analytics/hugot/pipelines"
	"github.com/moogacs/anonde/analyzer"
)

// HugotNERConfig configures the hugot-backed NER recognizer.
type HugotNERConfig struct {
	// ModelsDir is the local directory where models are stored.
	// Defaults to ~/.cache/anonde/models.
	ModelsDir string

	// ModelName is the HuggingFace model ID to use.
	// Defaults to "Isotonic/distilbert_finetuned_ai4privacy_v2" — a
	// distilbert NER fine-tuned on the ai4privacy PII corpus. Reaches
	// or beats Presidio default on every core entity (see
	// bench/parity/REPORT_FULL.md).
	ModelName string

	// AutoDownload, when true, downloads the model on first use if not present locally.
	AutoDownload bool

	// OnnxFilePath optionally selects a specific ONNX file inside the model
	// repo (e.g. "onnx/model_quantized.onnx" or "onnx/model_int8.onnx" for
	// faster inference at minor accuracy cost). Empty = repo default.
	// Only effective on the first download; cached models reuse whatever
	// was downloaded previously.
	OnnxFilePath string
}

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

	// ai4privacy: locations
	"CITY":             "LOCATION",
	"STATE":            "LOCATION",
	"COUNTY":           "LOCATION",
	"STREET":           "LOCATION",
	"BUILDINGNUMBER":   "LOCATION",
	"ZIPCODE":          "LOCATION",
	"SECONDARYADDRESS": "LOCATION",

	// ai4privacy: orgs
	"COMPANYNAME": "ORGANIZATION",
	"JOBTITLE":    "ORGANIZATION",
	"JOBAREA":     "ORGANIZATION",
	"JOBTYPE":     "ORGANIZATION",
}

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
		cfg.ModelName = "Isotonic/distilbert_finetuned_ai4privacy_v2"
	}
	return &HugotNERRecognizer{cfg: cfg}
}

func (r *HugotNERRecognizer) Name() string { return "HugotNERRecognizer" }
func (r *HugotNERRecognizer) SupportedEntities() []string {
	return []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"}
}
func (r *HugotNERRecognizer) SupportedLanguages() []string { return []string{"en"} }

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

	// Workaround for upstream hugot bug where certain text lengths trigger
	// a slice-bounds panic in gatherPreEntities. A trailing space changes
	// the tokenizer's alignment without altering the entity offsets we care
	// about (entities never include trailing whitespace).
	output, err := r.pipeline.RunPipeline(ctx, []string{text + " "})
	if err != nil {
		return nil, fmt.Errorf("hugot: run pipeline: %w", err)
	}

	if len(output.Entities) == 0 {
		return nil, nil
	}

	// Hugot's tokenizer returns codepoint (rune) offsets. The rest of anonde
	// is byte-indexed (Go strings, regex recognizers). Convert here so every
	// recognizer in the engine speaks the same offset units.
	r2b := buildRuneToByteIndex(text)
	for _, ent := range output.Entities[0] {
		entityType, ok := MapHugotLabel(ent.Entity)
		if !ok {
			continue
		}

		if !wantAll {
			if _, ok := want[entityType]; !ok {
				continue
			}
		}

		startByte := runeOffsetToByte(r2b, int(ent.Start))
		endByte := runeOffsetToByte(r2b, int(ent.End))
		results = append(results, analyzer.RecognizerResult{
			Start:          startByte,
			End:            endByte,
			Score:          float64(ent.Score),
			EntityType:     entityType,
			RecognizerName: r.Name(),
		})
	}
	return results, nil
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
