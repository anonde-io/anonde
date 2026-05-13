//go:build hugot

// probe_hugot — single-purpose diagnostic that answers "will hugot load
// model X?" without running a full bench.
//
// Background: anonde's HugotNERRecognizer maps a small set of known labels
// (CoNLL, ai4privacy, multilang-pii-ner) to anonde entity types and drops
// everything else. That's correct for production but misleading for a
// probe — a model that *did* load but emitted unfamiliar labels would
// look identical to one that produced nothing. This tool deliberately
// bypasses anonde's label filter and prints the RAW pipeline output
// (label, score, span) so the operator can see exactly what the model
// emitted before deciding whether a swap is worth the integration work.
//
// Single file, hugot-tagged. Run with:
//
//	go run -tags hugot ./bench/probes/hugot \
//	  --model urchade/gliner_multi-v2.1
//
// Useful for evaluating alternatives such as:
//   - onnx-community/multilang-pii-ner-ONNX     (current default — baseline)
//   - urchade/gliner_multi-v2.1                  (multilingual GLiNER)
//   - knowledgator/gliner-pii-base-v1.0          (PII-tuned GLiNER)
//   - Isotonic/distilbert_finetuned_ai4privacy_v2 (English PII distilbert)
//
// Exit codes:
//
//	0   model loaded and ran (regardless of detection quality)
//	1   model failed to load or run; stderr explains which stage failed
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/backends"
	"github.com/knights-analytics/hugot/pipelines"
)

// defaultProbeText is a synthetic German clinical sentence containing
// several PHI categories (person name, location, street, ZIP, date,
// phone). It is small enough to fit a 512-token context with margin and
// dense enough that any working clinical-PII model produces multiple
// hits — making "nothing returned" a strong negative signal.
const defaultProbeText = "Der Patient Herr Müller, geboren am 14.03.1962, wohnhaft Hauptstr. 8, 10115 Berlin, " +
	"wurde am 23.04.2026 in der Charité aufgenommen. Telefon: 030-12345678."

func main() {
	var (
		modelName    = flag.String("model", "onnx-community/multilang-pii-ner-ONNX", "HuggingFace model ID to probe")
		onnxFile     = flag.String("onnx-file", "onnx/model_quantized.onnx", "ONNX file inside the repo; empty = repo default")
		modelsDir    = flag.String("models-dir", "", "local cache dir (default ~/.cache/anonde/models)")
		autoDownload = flag.Bool("auto-download", true, "download from HF Hub if not cached")
		text         = flag.String("text", defaultProbeText, "input text to run inference on")
		minScore     = flag.Float64("min-score", 0.0, "only print entities with score >= this")
	)
	flag.Parse()

	if *modelsDir == "" {
		home, _ := os.UserHomeDir()
		*modelsDir = filepath.Join(home, ".cache", "anonde", "models")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log.SetFlags(0)
	log.SetPrefix("[probe] ")
	log.Printf("model           = %s", *modelName)
	log.Printf("onnx-file       = %s", emptyAs(*onnxFile, "<repo default>"))
	log.Printf("models-dir      = %s", *modelsDir)
	log.Printf("auto-download   = %v", *autoDownload)
	log.Printf("text (%d bytes) = %s", len(*text), truncate(*text, 120))

	// --- stage 1: session ---------------------------------------------------
	t0 := time.Now()
	session, err := hugot.NewGoSession(ctx)
	if err != nil {
		fatal("stage=session: %v", err)
	}
	defer session.Destroy()
	log.Printf("✓ session ready in %s", time.Since(t0).Round(time.Millisecond))

	// --- stage 2: download / locate model on disk ---------------------------
	// hugot.DownloadModel rewrites "/" → "_" in the cache path.
	modelPath := filepath.Join(*modelsDir, strings.ReplaceAll(*modelName, "/", "_"))
	if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
		if !*autoDownload {
			fatal("stage=locate: model not found at %s and --auto-download=false", modelPath)
		}
		if err := os.MkdirAll(*modelsDir, 0o755); err != nil {
			fatal("stage=locate: mkdir %s: %v", *modelsDir, err)
		}
		opts := hugot.NewDownloadOptions()
		if *onnxFile != "" {
			opts.OnnxFilePath = *onnxFile
		}
		t1 := time.Now()
		downloaded, err := hugot.DownloadModel(ctx, *modelName, *modelsDir, opts)
		if err != nil {
			fatal("stage=download: %v\n\n"+
				"Common causes:\n"+
				"  * repo doesn't ship an ONNX export (try a different --onnx-file or use an ONNX-community mirror)\n"+
				"  * the ONNX file path is wrong for this repo layout\n"+
				"  * network blocked; pre-download to %s manually", err, modelPath)
		}
		modelPath = downloaded
		log.Printf("✓ downloaded to %s in %s", modelPath, time.Since(t1).Round(time.Millisecond))
	} else {
		log.Printf("✓ found cached at %s", modelPath)
	}

	// --- stage 3: pipeline ---------------------------------------------------
	t2 := time.Now()
	pipeline, err := hugot.NewPipeline(session, hugot.TokenClassificationConfig{
		ModelPath: modelPath,
		Name:      "probe-ner",
		Options: []backends.PipelineOption[*pipelines.TokenClassificationPipeline]{
			pipelines.WithSimpleAggregation(),
			pipelines.WithIgnoreLabels([]string{"O"}),
		},
	})
	if err != nil {
		fatal("stage=pipeline: %v\n\n"+
			"Common causes:\n"+
			"  * model architecture isn't TokenClassification (GLiNER uses a different\n"+
			"    encoder/labelling shape — it loads but won't run through this pipeline);\n"+
			"  * tokenizer files (tokenizer.json) missing from the repo;\n"+
			"  * ONNX file references ops the pure-Go onnxruntime backend doesn't implement.\n\n"+
			"If GLiNER specifically: confirmed unsupported by the standard token-classification\n"+
			"pipeline — needs a custom hugot pipeline or a sidecar runner (Python/ORT).", err)
	}
	log.Printf("✓ pipeline ready in %s", time.Since(t2).Round(time.Millisecond))

	// --- stage 4: inference --------------------------------------------------
	// Trailing space mirrors anonde's workaround for an upstream hugot
	// tokenizer panic on certain text lengths.
	t3 := time.Now()
	output, err := pipeline.RunPipeline(ctx, []string{*text + " "})
	if err != nil {
		fatal("stage=inference: %v", err)
	}
	log.Printf("✓ inference in %s", time.Since(t3).Round(time.Millisecond))

	if len(output.Entities) == 0 || len(output.Entities[0]) == 0 {
		log.Printf("⚠ model returned 0 entities — loads but emits nothing on the probe text")
		log.Printf("  total elapsed: %s", time.Since(t0).Round(time.Millisecond))
		os.Exit(0)
	}

	// --- stage 5: report -----------------------------------------------------
	type row struct {
		Label string
		Text  string
		Score float64
		Start int
		End   int
	}
	rows := make([]row, 0, len(output.Entities[0]))
	for _, e := range output.Entities[0] {
		if float64(e.Score) < *minScore {
			continue
		}
		rows = append(rows, row{
			Label: e.Entity,
			Text:  e.Word,
			Score: float64(e.Score),
			Start: int(e.Start),
			End:   int(e.End),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Start < rows[j].Start
	})

	fmt.Printf("\n%-22s  %-7s  %-6s  %s\n", "LABEL", "SCORE", "SPAN", "TEXT")
	fmt.Println(strings.Repeat("─", 80))
	for _, r := range rows {
		fmt.Printf("%-22s  %5.3f    %4d-%-4d  %s\n",
			r.Label, r.Score, r.Start, r.End, r.Text)
	}
	fmt.Println()
	log.Printf("entities=%d  total elapsed=%s", len(rows), time.Since(t0).Round(time.Millisecond))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "✗ "+format+"\n", args...)
	os.Exit(1)
}

func emptyAs(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
