// runner_anonde reads a corpus JSONL, runs anonde on each doc, and
// writes a per-doc findings JSONL. Single source of truth across the
// bench matrix — every corpus's Makefile invokes this binary via
// `go run -tags hugot ./bench/runners/anonde.go ...`.
//
// Corpus shape is the contract:
//
//   corpus.jsonl:  {"id": "...", "text": "...", "entities": [...]}
//   anonde.jsonl:  {"id": "...", "engine": "...", "findings": [...], "duration_ms": ...}
//
// Offsets in findings are codepoint indices (Python convention), so the
// comparator can compare them against gold annotations that came from
// Python-tokenised sources (GraSCCo INCEpTION JSON, ai4privacy, etc.).
//
// Backends:
//
//   patterns-only   no NER (DisableNER=true) — fastest, no model
//   hugot           hugot/ONNX TokenClassification (XLM-R PII default)
//   gliner          GLiNER zero-shot NER, span decoder (knowledgator/gliner-pii-base-v1.0)
//   gliner-flat     GLiNER zero-shot NER, flat / token decoder
//                   (knowledgator/gliner-pii-large-v1.0 and other 4-input exports)
//
// Optional fold-for-parity mode (--fold-parity-labels) collapses
// STREET_ADDRESS / POSTAL_CODE to LOCATION; needed for the ai4privacy
// English bench whose gold buckets street + zip under LOCATION. The
// runtime keeps its precise categories for downstream consumers
// (e.g. the German corpus uses ADDRESS as a separate canonical type).

//go:build ignore

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

type goldDoc struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type finding struct {
	Start int     `json:"start"`
	End   int     `json:"end"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

type output struct {
	ID         string    `json:"id"`
	Engine     string    `json:"engine"`
	Findings   []finding `json:"findings"`
	DurationMS float64   `json:"duration_ms"`
}

func main() {
	var (
		inPath     = flag.String("in", "", "input corpus jsonl")
		outPath    = flag.String("out", "", "output findings jsonl")
		threshold  = flag.Float64("threshold", 0.3, "score threshold")
		language   = flag.String("language", "de", "AnalysisConfig.Language")
		backend    = flag.String("backend", "patterns-only", "hugot|gliner|gliner-flat|ollama|patterns-only")
		modelsDir  = flag.String("models-dir", "", "hugot models cache (default ~/.cache/anonde/models)")
		modelName  = flag.String("model", "", "hugot/gliner model id (empty = backend default)")
		onnxFile   = flag.String("onnx-file", "", "ONNX file path inside the HF repo (e.g. onnx/model_quantized.onnx); empty = repo default")
		scoreFloor = flag.Float64("ner-score-floor", 0, "drop NER predictions below this score before threshold filtering (0 = recognizer default, <0 = disabled)")
		glinerThr  = flag.Float64("gliner-threshold", 0, "gliner prediction threshold (0 = recognizer default, ~0.40)")
		ortLibPath = flag.String("ort-library", "", "onnxruntime shared library path (gliner backend; empty = system default)")
		autoDL     = flag.Bool("auto-download", true, "auto-download hugot model on first run")
		disableNER = flag.Bool("disable-ner", false, "force DisableNER=true regardless of backend")
		foldParity = flag.Bool("fold-parity-labels", false, "fold STREET_ADDRESS + POSTAL_CODE to LOCATION (ai4privacy gold schema)")

		ollamaEndpoint = flag.String("ollama-endpoint", "http://localhost:11434", "Ollama base URL (used by --backend ollama)")
		ollamaModel    = flag.String("ollama-model", "llama3.2:3b", "Ollama model tag (used by --backend ollama)")

		flatGLiNERModel = flag.String("flat-gliner-model", "", "additional flat-decoder GLiNER model id (e.g. knowledgator/gliner-pii-large-v1.0); registered alongside the base")
		flatGLiNEROnnx  = flag.String("flat-gliner-onnx", "", "ONNX file path inside the flat-GLiNER repo (e.g. model.onnx)")
		flatGLiNERThr   = flag.Float64("flat-gliner-threshold", 0, "flat-GLiNER threshold (0 = recognizer default)")
	)
	flag.Parse()
	if *inPath == "" || *outPath == "" {
		log.Fatal("--in and --out required")
	}

	var (
		engine      *analyzer.AnalyzerEngine
		engineLabel string
		nerOff      bool
	)
	switch *backend {
	case "hugot":
		engine = anonde.DefaultAnalyzerEngineWithHugotConfig(recognizers.HugotNERConfig{
			ModelsDir:    *modelsDir,
			ModelName:    *modelName,
			OnnxFilePath: *onnxFile,
			AutoDownload: *autoDL,
			ScoreFloor:   *scoreFloor,
		})
		engineLabel = "anonde-hugot"
		if *modelName != "" {
			engineLabel = "anonde-hugot[" + *modelName + "]"
		}
	case "gliner":
		engine = anonde.DefaultAnalyzerEngineWithGLiNERConfig(recognizers.GLiNERConfig{
			ModelsDir:         *modelsDir,
			ModelName:         *modelName,
			OnnxFilePath:      *onnxFile,
			AutoDownload:      *autoDL,
			Threshold:         *glinerThr,
			SharedLibraryPath: *ortLibPath,
		})
		engineLabel = "anonde-ner"
		if *modelName != "" {
			engineLabel = "anonde-ner[" + *modelName + "]"
		}
	case "gliner-flat":
		// Flat-decoder GLiNER (token-decoder variant — 4 ONNX inputs,
		// BIO start/end/inside output). Same registry shape as `gliner`
		// (all pattern recognizers + one NER recognizer), but the NER
		// slot is GLiNERFlatRecognizer. Kept as an opt-in backend for
		// ad-hoc runs against flat-decoder GLiNER variants
		// (knowledgator/gliner-pii-large-v1.0 ships a flat decoder; the
		// span-decoder recognizer used by `gliner` cannot load it). Not
		// referenced by the in-matrix engines today — `anonde-ner-stack`
		// uses backend=gliner with --flat-gliner-* flags instead.
		engine = anonde.DefaultAnalyzerEngineWithGLiNERFlatConfig(recognizers.GLiNERConfig{
			ModelsDir:         *modelsDir,
			ModelName:         *modelName,
			OnnxFilePath:      *onnxFile,
			AutoDownload:      *autoDL,
			Threshold:         *glinerThr,
			SharedLibraryPath: *ortLibPath,
		})
		engineLabel = "anonde-gliner-flat"
		if *modelName != "" {
			engineLabel = "anonde-gliner-flat[" + *modelName + "]"
		}
	case "patterns-only", "patterns":
		engine = anonde.DefaultAnalyzerEngine()
		nerOff = true
		engineLabel = "anonde-patterns-only"
	case "ollama":
		// Ollama as the NER backend. Pure-Go path (no CGO, no
		// libonnxruntime), uses a local Ollama daemon over HTTP.
		// Reuses the --ollama-endpoint / --ollama-model flags that
		// were previously reconciler-only. ONLY emits PERSON /
		// LOCATION / ORGANIZATION / NRP — pattern recognizers cover
		// the rest of the entity surface.
		ollMod := strings.TrimSpace(*ollamaModel)
		if ollMod == "" {
			ollMod = "llama3.2:3b"
		}
		engine = anonde.DefaultAnalyzerEngineWithOllama(*ollamaEndpoint, ollMod)
		engineLabel = "anonde-ollama[" + ollMod + "]"
	default:
		log.Fatalf("unknown --backend %q (valid: hugot, gliner, gliner-flat, ollama, patterns-only)", *backend)
	}
	if *disableNER {
		nerOff = true
	}

	// Optional second-stage GLiNER (token / flat decoder, e.g. LARGE).
	// Registers alongside the existing recognizers so both inferences
	// run per doc; the analyzer's RemoveConflicts merges overlaps. Only
	// wired for the `gliner` backend — patterns-only / hugot ignore it.
	if *backend == "gliner" && strings.TrimSpace(*flatGLiNERModel) != "" {
		flatRec := recognizers.NewGLiNERFlatRecognizer(recognizers.GLiNERConfig{
			ModelsDir:         *modelsDir,
			ModelName:         *flatGLiNERModel,
			OnnxFilePath:      *flatGLiNEROnnx,
			AutoDownload:      *autoDL,
			Threshold:         *flatGLiNERThr,
			SharedLibraryPath: *ortLibPath,
		})
		engine.Registry.Add(flatRec)
		engineLabel += "+flat[" + *flatGLiNERModel + "]"
		log.Printf("flat-gliner: registered alongside base (model=%s onnx=%q threshold=%.2f)",
			*flatGLiNERModel, *flatGLiNEROnnx, *flatGLiNERThr)
	}

	in, err := os.Open(*inPath)
	if err != nil {
		log.Fatalf("open input: %v", err)
	}
	defer in.Close()
	out, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create output: %v", err)
	}
	defer out.Close()

	cfg := analyzer.AnalysisConfig{
		Language:        *language,
		ScoreThreshold:  *threshold,
		RemoveConflicts: true,
		DisableNER:      nerOff,
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	docs := 0
	for scanner.Scan() {
		var doc goldDoc
		if err := json.Unmarshal(scanner.Bytes(), &doc); err != nil {
			log.Printf("skip malformed line: %v", err)
			continue
		}
		start := time.Now()
		results, err := engine.Analyze(context.Background(), doc.Text, cfg)
		if err != nil {
			log.Printf("analyze id=%s: %v", doc.ID, err)
			continue
		}
		// anonde recognizers report byte offsets; the gold corpus uses
		// codepoint offsets per Python convention. Convert here.
		findings := make([]finding, 0, len(results))
		for _, r := range results {
			ftype := r.EntityType
			if *foldParity {
				ftype = foldForParity(ftype)
			}
			findings = append(findings, finding{
				Start: utf8.RuneCountInString(doc.Text[:clamp(r.Start, len(doc.Text))]),
				End:   utf8.RuneCountInString(doc.Text[:clamp(r.End, len(doc.Text))]),
				Type:  ftype,
				Score: r.Score,
			})
		}
		dur := time.Since(start)
		_ = enc.Encode(output{
			ID:         doc.ID,
			Engine:     engineLabel,
			Findings:   findings,
			DurationMS: float64(dur.Microseconds()) / 1000.0,
		})
		docs++
		log.Printf("doc=%d id=%s spans=%d dur=%dms", docs, doc.ID, len(findings), dur.Milliseconds())
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("scan: %v", err)
	}
	log.Printf("processed %d docs (engine=%s, language=%s)", docs, engineLabel, *language)
}

// foldForParity normalises anonde's address-bucket entity types to LOCATION
// to match the ai4privacy gold schema. Other types pass through unchanged.
func foldForParity(t string) string {
	switch t {
	case "STREET_ADDRESS", "POSTAL_CODE":
		return "LOCATION"
	}
	return t
}

func clamp(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}
