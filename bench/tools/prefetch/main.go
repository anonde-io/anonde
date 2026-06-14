//go:build hugot

// Command prefetch deterministically downloads the GLiNER PII model
// exports the bench matrix needs and FAILS LOUD if any of them is
// missing, partial, or zero-byte.
//
// Why this exists (CI reliability, not a runtime path):
//
// The GLiNER recognizers (analyzer/recognizers/ner_gliner.go,
// ner_gliner_flat.go) auto-download their ONNX export on first use via
// hugot.DownloadModel into ~/.cache/anonde/models. If that download is
// slow, interrupted, or the cache was partially evicted, the recognizer
// init fails — and the analyzer SWALLOWS that error and falls back to
// patterns-only. In the bench matrix that surfaces as a cell that
// renders with "0 NER findings" (a silent PII leak), and the render
// guard rail then fails the whole run on a random subset of cells.
//
// Running this tool BEFORE the scored cell turns "model absent at
// runtime → silent fallback" into "download failed → step fails here,
// loudly, with a clear message." A gliner cell can then never run
// without its model: either prefetch succeeds and the model is on disk,
// or the job stops before scoring.
//
// It mirrors exactly what the recognizers do (same hugot.DownloadModel,
// same ~/.cache/anonde/models layout, same sanitizeModelName "/"→"_"
// mapping) so a successful prefetch guarantees the recognizer's own
// os.Stat(modelPath) check passes and it loads from cache instead of
// re-downloading.
//
// Usage:
//
//	go run -tags hugot ./bench/tools/prefetch \
//	    -model knowledgator/gliner-pii-base-v1.0  -onnx onnx/model.onnx \
//	    -model knowledgator/gliner-pii-large-v1.0 -onnx onnx/model.onnx
//
// Both knowledgator GLiNER PII exports ship their FP32 ONNX at
// onnx/model.onnx; the -onnx values MUST match GLINER_ONNX_FILE /
// GLINER_LARGE_ONNX_FILE in bench/Makefile.
//
// Repeated -model/-onnx pairs are matched by position. Exit code is 0
// only if every requested model resolved with a non-empty ONNX blob and
// a tokenizer.json on disk.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knights-analytics/hugot"
)

// modelList collects repeated -model flags in order.
type modelList []string

func (m *modelList) String() string { return strings.Join(*m, ",") }
func (m *modelList) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("prefetch: ")

	var models, onnxFiles modelList
	flag.Var(&models, "model", "HF model id to fetch (repeatable; matched by position with -onnx)")
	flag.Var(&onnxFiles, "onnx", "ONNX file path inside the matching repo (repeatable; e.g. onnx/model.onnx)")
	modelsDir := flag.String("models-dir", "", "local model cache (default ~/.cache/anonde/models — must match the recognizer)")
	timeout := flag.Duration("timeout", 20*time.Minute, "overall download deadline")
	flag.Parse()

	if len(models) == 0 {
		log.Fatal("at least one -model is required")
	}
	if len(onnxFiles) != 0 && len(onnxFiles) != len(models) {
		log.Fatalf("got %d -model and %d -onnx flags; -onnx must be omitted entirely or given once per -model",
			len(models), len(onnxFiles))
	}

	if *modelsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("resolve home dir: %v", err)
		}
		// Mirror analyzer/recognizers/ner_gliner.go's default exactly.
		*modelsDir = filepath.Join(home, ".cache", "anonde", "models")
	}
	if err := os.MkdirAll(*modelsDir, 0o755); err != nil {
		log.Fatalf("create models dir %s: %v", *modelsDir, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var failures []string
	for i, model := range models {
		onnx := ""
		if i < len(onnxFiles) {
			onnx = onnxFiles[i]
		}
		if err := fetchAndVerify(ctx, *modelsDir, model, onnx); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", model, err))
			log.Printf("FAILED  %s (%s): %v", model, onnxLabel(onnx), err)
			continue
		}
		log.Printf("OK      %s (%s)", model, onnxLabel(onnx))
	}

	if len(failures) > 0 {
		log.Printf("")
		log.Printf("%d of %d model(s) failed to prefetch:", len(failures), len(models))
		for _, f := range failures {
			log.Printf("  - %s", f)
		}
		log.Printf("")
		log.Printf("A gliner cell MUST NOT run without its model — the analyzer")
		log.Printf("would swallow the load error and silently fall back to")
		log.Printf("patterns-only, leaking PII and reddening the render guard rail.")
		log.Printf("Failing the step here instead. Common causes: HF Hub timeout,")
		log.Printf("a partial cache restore, or OOM during a concurrent model load.")
		os.Exit(1)
	}
	log.Printf("all %d model(s) present and verified under %s", len(models), *modelsDir)
}

func onnxLabel(onnx string) string {
	if onnx == "" {
		return "repo default onnx"
	}
	return onnx
}

// fetchAndVerify downloads model (if not already cached) and asserts the
// ONNX export and tokenizer.json are present and non-empty on disk. It
// uses the same ~/.cache/anonde/models/<sanitized>/ layout the GLiNER
// recognizer reads, so a pass here guarantees the recognizer loads from
// cache rather than re-downloading.
func fetchAndVerify(ctx context.Context, modelsDir, model, onnx string) error {
	// "/" → "_": matches sanitizeModelName in
	// analyzer/recognizers/ner_hugot.go.
	modelPath := filepath.Join(modelsDir, strings.ReplaceAll(model, "/", "_"))

	opts := hugot.NewDownloadOptions()
	if onnx != "" {
		opts.OnnxFilePath = onnx
	}
	// hugot.DownloadModel is a no-op fast path when the model already
	// exists, so calling it unconditionally also REPAIRS a partial cache
	// (the exact failure mode that produced random silent fallbacks).
	if _, err := hugot.DownloadModel(ctx, model, modelsDir, opts); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Verify the ONNX blob: present, regular file, non-zero size. The
	// large FP32 export is the one most likely to land truncated on a
	// memory- or disk-pressured runner.
	onnxRel := onnx
	if onnxRel == "" {
		// Recognizer default for the base export when none is given.
		onnxRel = filepath.Join("onnx", "model.onnx")
	}
	if err := verifyNonEmpty(filepath.Join(modelPath, onnxRel), "onnx export"); err != nil {
		return err
	}
	if err := verifyNonEmpty(filepath.Join(modelPath, "tokenizer.json"), "tokenizer"); err != nil {
		return err
	}
	return nil
}

func verifyNonEmpty(path, what string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s missing at %s: %w", what, path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("%s at %s is a directory, expected a file", what, path)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("%s at %s is zero-byte (partial/interrupted download)", what, path)
	}
	return nil
}
