//go:build ner

// Command prefetch downloads the GLiNER PII model exports the bench matrix
// needs and FAILS LOUD if any is missing, partial, or corrupt. It exists for
// CI reliability, not as a runtime path: the recognizers auto-download on
// first use, but a slow/interrupted/partially-evicted download makes init
// fail — and the analyzer swallows that and falls back to patterns-only,
// which the matrix only ever sees as a cell with "0 NER findings" (a silent
// PII leak). Running this before the scored cell turns "silent runtime
// fallback" into "download failed, step fails here loudly."
//
// It mirrors the recognizers exactly — same hugot.DownloadModel, same
// ~/.cache/anonde/models layout, same "/"→"_" sanitize, same ONNX resolution
// order (model.onnx at the model-dir root, else the -onnx hint, else the
// first *.onnx found by recursive walk). These HF repos are not necessarily
// laid out as onnx/model.onnx, so verification must NOT hard-require that one
// path or it fails-close while the recognizer loads fine off the walk fallback.
//
// Usage:
//
//	go run -tags ner ./bench/tools/prefetch \
//	    -model knowledgator/gliner-pii-base-v1.0  -onnx onnx/model.onnx \
//	    -model knowledgator/gliner-pii-large-v1.0 -onnx onnx/model.onnx
//
// Repeated -model/-onnx pairs are matched by position; -onnx is a download
// hint only. Exit 0 requires every model to resolve a non-empty ONNX +
// tokenizer.json AND for that ONNX to open as an onnxruntime session — the
// only check that catches a >0-byte but corrupt model.onnx (the "Protobuf
// parsing failed" failure a partial/raced HF download produces). The session
// check needs onnxruntime via ORT_SO_PATH; absent the library it is skipped
// and verification degrades to the size-only check.
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
	ort "github.com/yalue/onnxruntime_go"
)

// ortReady gates session verification; when false (no library on disk) we
// degrade to the size-only check rather than fail-close.
var ortInitErr error
var ortReady bool

// initORT points onnxruntime_go at ORT_SO_PATH (same var the recognizer reads)
// and initialises the process-wide environment once. A missing library is not
// fatal here — the workflow's separate libonnxruntime check already gates that
// — so we just degrade to the size-only check.
func initORT() {
	if libPath := os.Getenv("ORT_SO_PATH"); libPath != "" {
		ort.SetSharedLibraryPath(libPath)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "already been initialized") ||
			strings.Contains(msg, "already initialized") {
			ortReady = true
			return
		}
		ortInitErr = err
		return
	}
	ortReady = true
}

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

	// Init ORT so verifyModelFiles can open a session on each ONNX (the only
	// check that catches a >0-byte but corrupt model.onnx). Degrades if absent.
	initORT()
	if !ortReady {
		log.Printf("WARN    onnxruntime unavailable (%v); skipping ONNX session verification "+
			"(size-only check active — a corrupt-but-nonempty model.onnx could slip through)", ortInitErr)
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
	// analyzer/recognizers/ner_shared.go.
	modelPath := filepath.Join(modelsDir, strings.ReplaceAll(model, "/", "_"))

	opts := hugot.NewDownloadOptions()
	if onnx != "" {
		opts.OnnxFilePath = onnx
	}

	// hugot.DownloadModel no-ops when the model dir already exists, so a cache
	// hit is cheap. On a cache-cold run every matrix cell hits HF at once;
	// retry with backoff so a single transient 429/timeout doesn't redden a cell.
	if err := downloadWithRetry(ctx, model, modelsDir, opts); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Verify, and REPAIR a partial/empty cache. DownloadModel's skip-if-dir-
	// exists fast path also skips when the dir exists but is EMPTY — exactly
	// what a poisoned actions/cache restore produces: the static key was once
	// saved with a ~0-byte dir, and actions/cache never re-saves an existing
	// key, so every run restores the same empty stub and hugot skips the real
	// download (recognizer then silently falls back to patterns-only). See
	// bench-full runs 64ad360 + ae38561. Repair: purge the dir and re-download
	// so hugot has no dir to skip on.
	if err := verifyModelFiles(modelPath, onnx); err != nil {
		log.Printf("REPAIR  %s: cached dir incomplete (%v); purging and re-downloading", model, err)
		if rmErr := os.RemoveAll(modelPath); rmErr != nil {
			return fmt.Errorf("purge incomplete cache dir %s: %w", modelPath, rmErr)
		}
		if dlErr := downloadWithRetry(ctx, model, modelsDir, opts); dlErr != nil {
			return fmt.Errorf("re-download after purge: %w", dlErr)
		}
		if err := verifyModelFiles(modelPath, onnx); err != nil {
			return err
		}
	}
	return nil
}

// verifyModelFiles checks that modelPath holds a usable, non-empty ONNX export
// (resolved exactly as the recognizer resolves it — see resolveOnnx) and a
// non-empty tokenizer.json, and that the ONNX opens as an ORT session.
func verifyModelFiles(modelPath, onnx string) error {
	onnxFile, err := resolveOnnx(modelPath, onnx)
	if err != nil {
		return err
	}
	if err := verifyNonEmpty(onnxFile, "onnx export"); err != nil {
		return err
	}
	if err := verifyNonEmpty(filepath.Join(modelPath, "tokenizer.json"), "tokenizer"); err != nil {
		return err
	}
	// Load-bearing check: a size-only check passes a truncated/corrupt
	// model.onnx, which then dies at the recognizer's session open with
	// "Protobuf parsing failed" AFTER prefetch reported OK (bench-full run
	// 27504028826: legal_de + ai4privacy_de). Surfacing it here lets
	// fetchAndVerify's purge-and-re-download repair the corrupt cache in-run.
	if err := verifySession(onnxFile); err != nil {
		return err
	}
	return nil
}

// verifySession opens (and destroys) a throwaway ORT session on onnxFile,
// forcing onnxruntime to parse the model protobuf — the step that fails on a
// corrupt/truncated export but which a size check waves through. Skipped when
// ORT is unavailable (see initORT). Uses the recognizer's exact input/output
// names so the open path is identical to ner_gliner.go's.
func verifySession(onnxFile string) error {
	if !ortReady {
		return nil
	}
	inputNames := []string{
		"input_ids",
		"attention_mask",
		"words_mask",
		"text_lengths",
		"span_idx",
		"span_mask",
	}
	outputNames := []string{"logits"}
	session, err := ort.NewDynamicAdvancedSession(onnxFile, inputNames, outputNames, nil)
	if err != nil {
		return fmt.Errorf("onnx session open failed (model present but unparseable — corrupt/partial download): %w", err)
	}
	_ = session.Destroy()
	return nil
}

// downloadWithRetry wraps hugot.DownloadModel with bounded exponential
// backoff: on a cache-cold run every matrix cell hits HF at once and a
// fraction draw a 429/reset/timeout. Total time stays bounded by ctx (-timeout).
func downloadWithRetry(ctx context.Context, model, modelsDir string, opts hugot.DownloadOptions) error {
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if _, err := hugot.DownloadModel(ctx, model, modelsDir, opts); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return fmt.Errorf("download deadline reached after %d attempt(s): %w", attempt, lastErr)
		}
		if attempt < maxAttempts {
			backoff := time.Duration(attempt*attempt) * 5 * time.Second
			log.Printf("RETRY   %s: download attempt %d/%d failed (%v); retrying in %s",
				model, attempt, maxAttempts, lastErr, backoff)
			select {
			case <-ctx.Done():
				return fmt.Errorf("download deadline reached during backoff: %w", lastErr)
			case <-time.After(backoff):
			}
		}
	}
	return fmt.Errorf("download failed after %d attempts: %w", maxAttempts, lastErr)
}

// resolveOnnx mirrors the GLiNER recognizer's ONNX resolution order and
// returns the path it would load. It only errors if NO usable onnx is
// found anywhere under modelPath (fail-loud: a download that landed
// without an onnx is still a hard failure).
func resolveOnnx(modelPath, onnxHint string) (string, error) {
	if root := filepath.Join(modelPath, "model.onnx"); statRegular(root) {
		return root, nil
	}
	if onnxHint != "" {
		if candidate := filepath.Join(modelPath, onnxHint); statRegular(candidate) {
			return candidate, nil
		}
	}
	// Fallback: any *.onnx under modelPath — replicates findFirstOnnx in
	// ner_gliner.go (deterministic walk order).
	if found, err := findFirstOnnx(modelPath); err == nil && found != "" {
		return found, nil
	}
	return "", fmt.Errorf("no *.onnx found under %s (checked root model.onnx, %s, and a recursive walk)",
		modelPath, onnxLabel(onnxHint))
}

// statRegular reports whether path exists and is a regular file.
func statRegular(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// findFirstOnnx walks dir and returns the first *.onnx file (deterministic
// walk order). Replicated from analyzer/recognizers/ner_gliner.go so the
// prefetch verifier accepts exactly what the recognizer accepts.
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
