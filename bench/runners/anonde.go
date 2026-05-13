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
//   gliner          GLiNER zero-shot NER (knowledgator/gliner-pii-base-v1.0)
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
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"time"
	"unicode/utf8"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/auditor"
	"github.com/anonde-io/anonde/analyzer/recognizers"
	"github.com/anonde-io/anonde/analyzer/reconciler"
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
		backend    = flag.String("backend", "patterns-only", "hugot|gliner|patterns-only")
		modelsDir  = flag.String("models-dir", "", "hugot models cache (default ~/.cache/anonde/models)")
		modelName  = flag.String("model", "", "hugot/gliner model id (empty = backend default)")
		onnxFile   = flag.String("onnx-file", "", "ONNX file path inside the HF repo (e.g. onnx/model_quantized.onnx); empty = repo default")
		scoreFloor = flag.Float64("ner-score-floor", 0, "drop NER predictions below this score before threshold filtering (0 = recognizer default, <0 = disabled)")
		glinerThr  = flag.Float64("gliner-threshold", 0, "gliner prediction threshold (0 = recognizer default, ~0.40)")
		ortLibPath = flag.String("ort-library", "", "onnxruntime shared library path (gliner backend; empty = system default)")
		autoDL     = flag.Bool("auto-download", true, "auto-download hugot model on first run")
		disableNER = flag.Bool("disable-ner", false, "force DisableNER=true regardless of backend")
		foldParity = flag.Bool("fold-parity-labels", false, "fold STREET_ADDRESS + POSTAL_CODE to LOCATION (ai4privacy gold schema)")

		reconcilerKind = flag.String("reconciler", "none", "none|ollama")
		ollamaEndpoint = flag.String("ollama-endpoint", "http://localhost:11434", "Ollama base URL")
		ollamaModel    = flag.String("ollama-model", "llama3.2:3b", "Ollama model tag for reconciler")
		recLow         = flag.Float64("reconciler-low", 0.40, "reconciler low gate")
		recHigh        = flag.Float64("reconciler-high", 0.85, "reconciler high gate")
		recWorkers     = flag.Int("reconciler-workers", 4, "max concurrent LLM requests")
		recTimeoutSec  = flag.Int("reconciler-timeout-sec", 5, "per-span LLM call timeout")

		auditorKind     = flag.String("auditor", "none", "none|ollama")
		auditorModel    = flag.String("auditor-model", "llama3.1:8b", "Ollama model for the auditor")
		auditorTimeout  = flag.Int("auditor-timeout-sec", 60, "auditor per-doc timeout")
		auditorMaxChars = flag.Int("auditor-max-chars", 8000, "auditor truncates docs longer than this")
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
		engineLabel = "anonde-gliner"
		if *modelName != "" {
			engineLabel = "anonde-gliner[" + *modelName + "]"
		}
	case "patterns-only", "patterns":
		engine = anonde.DefaultAnalyzerEngine()
		nerOff = true
		engineLabel = "anonde-patterns-only"
	default:
		log.Fatalf("unknown --backend %q (valid: hugot, gliner, patterns-only)", *backend)
	}
	if *disableNER {
		nerOff = true
	}

	var ollamaRec *reconciler.Ollama
	switch *reconcilerKind {
	case "", "none":
	case "ollama":
		ollamaRec = reconciler.NewOllama(reconciler.OllamaConfig{
			Endpoint:      *ollamaEndpoint,
			Model:         *ollamaModel,
			LowGate:       *recLow,
			HighGate:      *recHigh,
			MaxConcurrent: *recWorkers,
			Timeout:       time.Duration(*recTimeoutSec) * time.Second,
		})
		engine.Reconciler = ollamaRec
		engineLabel += "+ollama-reconciler"
		log.Printf("reconciler=ollama model=%s gates=[%.2f,%.2f) workers=%d timeout=%ds",
			*ollamaModel, *recLow, *recHigh, *recWorkers, *recTimeoutSec)
		warmupOllama(*ollamaEndpoint, *ollamaModel)
	default:
		log.Fatalf("unknown --reconciler %q (valid: none, ollama)", *reconcilerKind)
	}

	switch *auditorKind {
	case "", "none":
	case "ollama":
		anonde.WithOllamaAuditor(engine, auditor.OllamaConfig{
			Endpoint:      *ollamaEndpoint,
			Model:         *auditorModel,
			Timeout:       time.Duration(*auditorTimeout) * time.Second,
			MaxInputChars: *auditorMaxChars,
		})
		engineLabel += "+ollama-auditor"
		log.Printf("auditor=ollama model=%s timeout=%ds max-chars=%d",
			*auditorModel, *auditorTimeout, *auditorMaxChars)
		warmupOllama(*ollamaEndpoint, *auditorModel)
	default:
		log.Fatalf("unknown --auditor %q (valid: none, ollama)", *auditorKind)
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

	if ollamaRec != nil {
		s := ollamaRec.Stats()
		log.Printf("reconciler stats: total=%d kept_high=%d dropped_low=%d llm_band=%d llm_keep=%d llm_drop=%d llm_error=%d cache_hit=%d",
			s.Total, s.KeptHigh, s.DroppedLow, s.LLMBand, s.LLMKeep, s.LLMDrop, s.LLMError, s.CacheHit)
	}
}

// warmupOllama makes one trivial chat request so the model is loaded
// into Ollama's memory before the bench loop's concurrent calls. Without
// this, the first batch of reconciler / auditor calls all wait for a
// cold load (10–30 s) and time out together.
func warmupOllama(endpoint, model string) {
	body := []byte(`{"model":"` + model + `","stream":false,` +
		`"options":{"num_predict":1,"temperature":0},` +
		`"messages":[{"role":"user","content":"ok"}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		log.Printf("warmup: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("warmup: %v (continuing — calls will fail-open until model is loaded)", err)
		return
	}
	_ = resp.Body.Close()
	log.Printf("warmup: model %s loaded in %dms", model, time.Since(t0).Milliseconds())
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
