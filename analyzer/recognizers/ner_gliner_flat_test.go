//go:build ner

package recognizers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anonde-io/anonde/analyzer/recognizers"
	"github.com/anonde-io/anonde/anonymizer"
)

// TestGLiNERFlatRecognizer_Metadata verifies the recognizer name (and
// its NERRecognizer suffix so DisableNER controls it) plus the canonical
// entities surfaced via SupportedEntities. Pure-Go assertion; works in
// every CI env regardless of cached model.
func TestGLiNERFlatRecognizer_Metadata(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewGLiNERFlatRecognizer(recognizers.GLiNERConfig{})
	if got := rec.Name(); got != "GLiNERFlatNERRecognizer" {
		t.Fatalf("Name() = %q, want GLiNERFlatNERRecognizer", got)
	}
	if !strings.HasSuffix(rec.Name(), "NERRecognizer") {
		t.Errorf("recognizer name %q must end in NERRecognizer to obey DisableNER", rec.Name())
	}
	got := rec.SupportedEntities()
	gotSet := make(map[string]bool, len(got))
	for _, e := range got {
		gotSet[e] = true
	}
	// The empty config resolves to DefaultLabelToEntity (= chat). DATE_TIME
	// is clinical-only now; callers that need it pass ClinicalPIILabels.
	for _, want := range []string{"PERSON", "LOCATION", "STREET_ADDRESS", "PHONE_NUMBER", "EMAIL_ADDRESS"} {
		if !gotSet[want] {
			t.Errorf("SupportedEntities missing %q (got %v)", want, got)
		}
	}
}

// TestGLiNERFlat_MissingModelNoDownload covers the AutoDownload=false
// path independent of any cached model; this runs in every CI env.
func TestGLiNERFlat_MissingModelNoDownload(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewGLiNERFlatRecognizer(recognizers.GLiNERConfig{
		ModelsDir:    t.TempDir(),
		ModelName:    "knowledgator/gliner-pii-large-v1.0",
		AutoDownload: false,
	})
	_, err := rec.Analyze(context.Background(), "Contact John Doe about it.", nil, "en")
	if err == nil {
		t.Fatal("expected error when model is absent and AutoDownload is false")
	}
}

// TestGLiNERFlat_PersonBreadthSmoke exercises the full flat-decoder
// inference path on the canonical PersonBreadth fixture. Skips cleanly
// when the LARGE model OR libonnxruntime isn't cached locally; does NOT
// trigger a network download in tests.
func TestGLiNERFlat_PersonBreadthSmoke(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir: %v", err)
	}
	modelsDir := filepath.Join(home, ".cache", "anonde", "models")
	modelPath := filepath.Join(modelsDir, "knowledgator_gliner-pii-large-v1.0")
	if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
		t.Skipf("gliner-large model not cached at %s; skipping smoke test", modelPath)
	}
	// Layout note: the LARGE export ships model.onnx at the repo root
	// (no `onnx/` subdir, unlike base).
	onnxPath := filepath.Join(modelPath, "model.onnx")
	if _, statErr := os.Stat(onnxPath); os.IsNotExist(statErr) {
		t.Skipf("gliner-large model.onnx missing under %s; skipping smoke test", modelPath)
	}

	// Locate libonnxruntime; same dev fallbacks as the span recognizer
	// test so the same machines green-light both.
	libPath := os.Getenv("ORT_LIBRARY_PATH")
	if libPath == "" {
		wd, _ := os.Getwd()
		repo := filepath.Clean(filepath.Join(wd, "..", ".."))
		for _, candidate := range []string{
			filepath.Join(repo, ".tokenlib", "libonnxruntime.dylib"),
			filepath.Join(repo, ".venv-bench", "lib", "python3.12", "site-packages", "onnxruntime", "capi", "libonnxruntime.1.26.0.dylib"),
			"/opt/homebrew/lib/libonnxruntime.dylib",
		} {
			if _, err := os.Stat(candidate); err == nil {
				libPath = candidate
				break
			}
		}
	}

	rec := recognizers.NewGLiNERFlatRecognizer(recognizers.GLiNERConfig{
		ModelsDir:         modelsDir,
		ModelName:         "knowledgator/gliner-pii-large-v1.0",
		OnnxFilePath:      "model.onnx",
		AutoDownload:      false,
		SharedLibraryPath: libPath,
	})
	defer func() { _ = rec.Destroy() }()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const text = "Contact John Doe about it."
	results, err := rec.Analyze(ctx, text, nil, "en")
	if err != nil {
		// libonnxruntime may not be installed on this host. Skip rather
		// than fail; this smoke test is opportunistic, not gating.
		if strings.Contains(err.Error(), "Platform-specific initialization failed") ||
			strings.Contains(err.Error(), "shared library") {
			t.Skipf("onnxruntime shared library not available: %v", err)
		}
		t.Fatalf("Analyze: %v", err)
	}

	// At least one PERSON span covering some part of "John Doe".
	merged := anonymizer.MergeAdjacentSameType(results, text)
	wantStart := strings.Index(text, "John Doe")
	if wantStart < 0 {
		t.Fatalf("setup: expected name not found in input")
	}
	wantEnd := wantStart + len("John Doe")
	covered := false
	for _, r := range merged {
		if r.EntityType != "PERSON" {
			continue
		}
		// Any overlap with the John Doe range counts; the wider-span
		// tiebreak should yield full-name coverage but we tolerate
		// partial coverage rather than fail the smoke check on a model
		// quirk; the strict per-fixture coverage check lives in the
		// span-decoder test suite.
		if r.Start < wantEnd && r.End > wantStart {
			covered = true
			break
		}
	}
	if !covered {
		t.Errorf("no PERSON span covered any char of %q in %q; results=%+v", "John Doe", text, results)
	}
}
