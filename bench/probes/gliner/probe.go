//go:build ner

// probe_gliner — diagnostic harness for the Go-native GLiNER
// recognizer (analyzer/recognizers.GLiNERRecognizer). Loads the model
// once, runs inference on a single text input, and prints the decoded
// entities. Useful when the model returns 0 results or when debugging
// chunking / tokenizer issues.
//
// Run with:
//
//	go run -tags ner ./bench/probes/gliner --text "..."
//
// Flags:
//
//	--text             input text (defaults to a synthetic German clinical sentence)
//	--threshold        GLiNER prediction threshold (default 0.40)
//	--ort-library      path to libonnxruntime.dylib; empty = auto-probe
//	--models-dir       local model cache (empty = ~/.cache/anonde/models)
//	--via-engine       wrap in the full AnalyzerEngine pipeline (incl. patterns)
//	--debug            write token-level diagnostics to ./gliner_debug.log

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

func main() {
	var (
		text     = flag.String("text", "Der Patient Herr Müller, geboren am 14.03.1962, wohnhaft Hauptstr. 8, 10115 Berlin, wurde am 23.04.2026 in der Charité aufgenommen. Telefon: 030-12345678.", "input text")
		thresh   = flag.Float64("threshold", 0.40, "GLiNER threshold")
		libPath  = flag.String("ort-library", "", "onnxruntime shared library path (empty = autodetect)")
		modelDir = flag.String("models-dir", "", "models directory (empty = default)")
		modelID  = flag.String("model", "", "HuggingFace model ID (empty = recognizer default)")
		onnxFile = flag.String("onnx-file", "", "ONNX file path inside the repo (empty = recognizer default)")
		viaEng   = flag.Bool("via-engine", false, "wrap in the full AnalyzerEngine pipeline")
		debug    = flag.Bool("debug", false, "write token-level diagnostics to ./gliner_debug.log")
	)
	flag.Parse()

	if *libPath == "" {
		repo, _ := os.Getwd()
		for _, c := range []string{
			filepath.Join(repo, ".tokenlib", "libonnxruntime.dylib"),
			filepath.Join(repo, ".venv-bench", "lib", "python3.12", "site-packages", "onnxruntime", "capi", "libonnxruntime.1.26.0.dylib"),
		} {
			if _, err := os.Stat(c); err == nil {
				*libPath = c
				break
			}
		}
	}

	if *debug {
		recognizers.GLiNERDebug = true
	}

	cfg := recognizers.GLiNERConfig{
		ModelsDir:         *modelDir,
		ModelName:         *modelID,
		OnnxFilePath:      *onnxFile,
		Threshold:         *thresh,
		SharedLibraryPath: *libPath,
		AutoDownload:      true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t0 := time.Now()

	if *viaEng {
		// Full pipeline: GLiNER + pattern recognizers, with conflict
		// resolution. Mirrors how the bench (`runner_anonde.go
		// --backend gliner`) invokes the recognizer.
		engine := anonde.DefaultAnalyzerEngineWithGLiNERConfig(cfg)
		results, err := engine.Analyze(ctx, *text, analyzer.AnalysisConfig{
			Language:        "de",
			ScoreThreshold:  0.3,
			RemoveConflicts: true,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Engine.Analyze failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("text (%d bytes): %s\n", len(*text), *text)
		fmt.Printf("engine total = %s\n", time.Since(t0))
		fmt.Printf("entities = %d\n", len(results))
		for _, r := range results {
			fmt.Printf("  [%d:%d] %-16s %.3f  %q\n", r.Start, r.End, r.EntityType, r.Score, (*text)[r.Start:r.End])
		}
		return
	}

	// GLiNER-only path — useful when the patterns mask out the model
	// output and you want to see what the model actually emitted.
	rec := recognizers.NewGLiNERRecognizer(cfg)
	defer rec.Destroy()
	results, err := rec.Analyze(ctx, *text, nil, "de")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Analyze failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("text (%d bytes): %s\n", len(*text), *text)
	fmt.Printf("inference took %s\n", time.Since(t0))
	fmt.Printf("entities = %d\n", len(results))
	for _, r := range results {
		fmt.Printf("  [%d:%d] %-16s %.3f  %q\n", r.Start, r.End, r.EntityType, r.Score, (*text)[r.Start:r.End])
	}
}
