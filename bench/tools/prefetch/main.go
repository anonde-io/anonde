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
// mapping) so a successful prefetch guarantees the recognizer's own ONNX
// resolution passes and it loads from cache instead of re-downloading.
//
// Verification mirrors the recognizer's ONNX resolution order
// (analyzer/recognizers/ner_gliner.go): model.onnx at the model-dir root,
// else the requested -onnx path (a HINT, not a hard requirement), else
// the first *.onnx found by a recursive walk. The on-disk layout of these
// HF repos is not necessarily onnx/model.onnx, so the verifier must NOT
// hard-require that single path — it would fail-close while the recognizer
// loads fine off the walk fallback.
//
// Usage:
//
//	go run -tags hugot ./bench/tools/prefetch \
//	    -model knowledgator/gliner-pii-base-v1.0  -onnx onnx/model.onnx \
//	    -model knowledgator/gliner-pii-large-v1.0 -onnx onnx/model.onnx
//
// The -onnx values SHOULD match GLINER_ONNX_FILE / GLINER_LARGE_ONNX_FILE
// in bench/Makefile so the resolution order matches the recognizer, but
// they are a download/order hint only — verification falls back to any
// usable *.onnx on disk just like the recognizer does.
//
// Repeated -model/-onnx pairs are matched by position. Exit code is 0
// only if every requested model resolved with a non-empty ONNX blob and
// a tokenizer.json on disk AND that ONNX opens as an onnxruntime session
// (the only check that catches a >0-byte but corrupt/truncated model.onnx —
// the "Protobuf parsing failed" load failure a partial/raced HF download
// produces). The session check needs onnxruntime; set ORT_SO_PATH to the
// shared library (the bench workflow already does). If the library is
// absent the session check is skipped and verification degrades to the
// legacy size-only check.
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

// ortInitErr records the one-time onnxruntime environment init result.
// A nil ortInitErr means the ONNX session-open verification (see
// verifySession) is active; a non-nil one means we could not set up ORT
// (no library on disk) and fall back to the size-only check.
var ortInitErr error
var ortReady bool

// initORT points onnxruntime_go at the shared library named by ORT_SO_PATH
// (set by .github/workflows/bench-full.yml, same var the recognizer reads)
// and initialises the process-wide environment exactly once. If the library
// is absent we record the error and skip session verification rather than
// fail the prefetch — the workflow's separate "Verify libonnxruntime is
// present" step already fails loud on a missing .so, so here a missing lib
// just degrades to the legacy size-only check.
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

	// Initialise onnxruntime so verifyModelFiles can open an ORT session
	// on each resolved ONNX — the ONLY check that catches a >0-byte but
	// CORRUPT model.onnx (the "Protobuf parsing failed" load failure that
	// a partial/raced HF download produces and the size-only check waves
	// through). If the library is unavailable we degrade gracefully.
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
	// analyzer/recognizers/ner_hugot.go.
	modelPath := filepath.Join(modelsDir, strings.ReplaceAll(model, "/", "_"))

	opts := hugot.NewDownloadOptions()
	if onnx != "" {
		opts.OnnxFilePath = onnx
	}

	// First attempt. hugot.DownloadModel has a "model dir already exists →
	// skip" fast path, so a normal cache hit is a cheap no-op here. On a
	// cache-cold run (e.g. the first run after a cache-key bump) all matrix
	// cells download from HF concurrently; retry with backoff so a single
	// transient 429 / timeout doesn't redden a cell.
	if err := downloadWithRetry(ctx, model, modelsDir, opts); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Verify, and REPAIR a partial/empty cache. The first DownloadModel
	// above no-ops when the model DIRECTORY exists, even if that directory
	// is empty — which is exactly what a poisoned actions/cache restore
	// produces (the static cache key was once saved with a ~0-byte dir, and
	// actions/cache never re-saves a key that already exists, so every run
	// restores the same empty stub and hugot skips the real download). The
	// recognizer then silently falls back to patterns-only, leaking PII.
	// See bench-full runs 64ad360 (canary caught meddocan_es/synth_finance_es
	// at 0 NER findings) and ae38561 (every cell failed prefetch).
	//
	// Repair: if the cache stub is missing a usable onnx OR the tokenizer,
	// purge the model dir and download once more — this time hugot has no
	// existing dir to skip on, so it actually fetches the files.
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

// verifyModelFiles checks that modelPath holds a usable, non-empty ONNX
// export (resolved the same way the GLiNER recognizer resolves it) and a
// non-empty tokenizer.json. Returns an error describing the first missing
// artifact, or nil when the model is complete.
//
// ONNX resolution mirrors the recognizer (analyzer/recognizers/ner_gliner.go
// ~514-528): model.onnx at the model-dir root, else the requested -onnx
// path (a hint, not a hard requirement), else the first *.onnx found by a
// recursive walk. The on-disk layout of these HF repos is NOT necessarily
// onnx/model.onnx, which is why a single hard-coded path fail-closed while
// the recognizer loaded fine off the walk fallback.
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
	// The load-bearing check: open an ORT session on the resolved ONNX,
	// exactly as analyzer/recognizers/ner_gliner.go does. A size-only
	// check passes a truncated/corrupt model.onnx (a partial or raced HF
	// download leaves a >0-byte file whose protobuf graph is incomplete),
	// which then dies at the recognizer's NewDynamicAdvancedSession with
	// "Protobuf parsing failed" — a fail-loud cell crash AFTER prefetch
	// reported OK (bench-full run 27504028826: legal_de + ai4privacy_de).
	// Surfacing that here as a verify error makes fetchAndVerify's
	// purge-and-re-download repair fix the corrupt cache in-run.
	if err := verifySession(onnxFile); err != nil {
		return err
	}
	return nil
}

// verifySession opens a throwaway ORT session on onnxFile with the same
// input/output names the GLiNER recognizer uses (ner_gliner.go ~534-542),
// then immediately destroys it. This forces onnxruntime to parse the model
// protobuf — the step that fails on a corrupt/truncated export. A nil
// return means the recognizer will load this file; any error means the
// on-disk model is unusable despite being present and non-empty.
//
// If onnxruntime could not be initialised (no shared library on disk),
// verification is skipped — the workflow's dedicated libonnxruntime check
// already gates that case, and we don't want to fail-close on a missing lib
// when the model itself may be fine.
func verifySession(onnxFile string) error {
	if !ortReady {
		return nil
	}
	// Mirror ner_gliner.go's session input/output names exactly so the
	// session-open path is identical to the recognizer's.
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
// backoff. On a cache-cold run every matrix cell hammers the HF Hub at
// once for the same ~0.5 GB base + ~1.7 GB large export, and a fraction
// reliably draw a 429 / connection-reset / read timeout. A single such
// blip used to fail the whole cell (and pre-fail-loud, leak silently); a
// few retries turn it into a momentary stall. The download deadline is
// the shared ctx, so total time is still bounded by -timeout.
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
	// 1. model.onnx at the root.
	if root := filepath.Join(modelPath, "model.onnx"); statRegular(root) {
		return root, nil
	}
	// 2. the requested -onnx path, treated as a hint.
	if onnxHint != "" {
		if candidate := filepath.Join(modelPath, onnxHint); statRegular(candidate) {
			return candidate, nil
		}
	}
	// 3. recursive walk for any *.onnx — replicates findFirstOnnx in
	// analyzer/recognizers/ner_gliner.go (deterministic walk order).
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
