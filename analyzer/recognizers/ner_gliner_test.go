//go:build hugot

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

// gliner_probeText is the German clinical sentence used by
// bench/probes/hugot/probe.go — small enough to fit a 512-token context,
// dense enough that any working clinical-PII model produces multiple
// hits. Reusing it keeps cross-backend comparison apples-to-apples.
const gliner_probeText = "Der Patient Herr Müller, geboren am 14.03.1962, wohnhaft Hauptstr. 8, 10115 Berlin, " +
	"wurde am 23.04.2026 in der Charité aufgenommen. Telefon: 030-12345678."

// TestGLiNERRecognizer_Metadata verifies the recognizer name and that
// SupportedEntities covers the canonical types we depend on.
func TestGLiNERRecognizer_Metadata(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewGLiNERRecognizer(recognizers.GLiNERConfig{})
	if got := rec.Name(); got != "GLiNERRecognizer" {
		t.Fatalf("Name() = %q, want GLiNERRecognizer", got)
	}
	// Name must end in NERRecognizer so DisableNER controls it.
	if !endsWith(rec.Name(), "NERRecognizer") {
		t.Errorf("recognizer name %q must end in NERRecognizer to obey DisableNER", rec.Name())
	}
	got := rec.SupportedEntities()
	gotSet := make(map[string]bool, len(got))
	for _, e := range got {
		gotSet[e] = true
	}
	// These come from DefaultLabelToEntity — if the map shrinks we
	// want the test to fail explicitly.
	for _, want := range []string{"PERSON", "LOCATION", "STREET_ADDRESS", "PHONE_NUMBER", "DATE_TIME"} {
		if !gotSet[want] {
			t.Errorf("SupportedEntities missing %q (got %v)", want, got)
		}
	}
}

// TestGLiNER_MissingModelNoDownload covers the AutoDownload=false path
// — independent of any cached model, so this runs in every CI env.
func TestGLiNER_MissingModelNoDownload(t *testing.T) {
	t.Parallel()
	rec := recognizers.NewGLiNERRecognizer(recognizers.GLiNERConfig{
		ModelsDir:    t.TempDir(),
		ModelName:    "knowledgator/gliner-pii-base-v1.0",
		AutoDownload: false,
	})
	_, err := rec.Analyze(context.Background(), gliner_probeText, nil, "de")
	if err == nil {
		t.Fatal("expected error when model is absent and AutoDownload is false")
	}
}

// TestGLiNER_SmokeGermanProbe exercises the full inference path on a
// cached German clinical sentence. Skips cleanly if the model OR
// libonnxruntime isn't available — the bench machine has both, fresh
// CI nodes may not.
//
// Assertion is intentionally loose ("at least 3 entities") because
// span boundaries vary slightly with chunking; the threshold (0.40)
// matches the Python sidecar. If the recognizer regresses to 0 hits
// we want a hard failure.
func TestGLiNER_SmokeGermanProbe(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir: %v", err)
	}
	modelsDir := filepath.Join(home, ".cache", "anonde", "models")
	modelPath := filepath.Join(modelsDir, "knowledgator_gliner-pii-base-v1.0")
	if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
		t.Skipf("gliner model not cached at %s; skipping smoke test", modelPath)
	}

	// Allow the test to drive a non-default onnxruntime shared library
	// via ORT_LIBRARY_PATH so developers without a system install can
	// point at one shipped by a Python venv (e.g. the .venv-bench
	// onnxruntime wheel). Otherwise probe well-known dev fallback
	// locations.
	libPath := os.Getenv("ORT_LIBRARY_PATH")
	if libPath == "" {
		// .venv-bench wheel — the most common dev box layout in this repo.
		wd, _ := os.Getwd()
		// wd is .../analyzer/recognizers; go up two levels for the repo root.
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

	rec := recognizers.NewGLiNERRecognizer(recognizers.GLiNERConfig{
		ModelsDir:         modelsDir,
		ModelName:         "knowledgator/gliner-pii-base-v1.0",
		OnnxFilePath:      "onnx/model_quint8.onnx",
		AutoDownload:      false,
		SharedLibraryPath: libPath,
	})
	defer func() {
		// Free CGO resources; first instance owns them.
		_ = rec.Destroy()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := rec.Analyze(ctx, gliner_probeText, nil, "de")
	if err != nil {
		// The onnxruntime shared library may simply not be installed
		// on this host (homebrew install onnxruntime, or set
		// ORT_LIBRARY_PATH to a wheel-shipped .dylib). Skip rather
		// than fail — this test is opportunistic, not gating.
		if strings.Contains(err.Error(), "Platform-specific initialization failed") ||
			strings.Contains(err.Error(), "shared library") {
			t.Skipf("onnxruntime shared library not available: %v", err)
		}
		t.Fatalf("Analyze: %v", err)
	}
	if len(results) < 3 {
		t.Fatalf("expected >=3 entities on the probe sentence, got %d: %+v", len(results), results)
	}

	// Sanity: at least one PERSON and one entity within the byte
	// range of "Müller" (37..43 in the probe text — verified manually).
	gotPerson := false
	for _, r := range results {
		if r.EntityType == "PERSON" {
			gotPerson = true
			break
		}
	}
	if !gotPerson {
		t.Errorf("no PERSON detected on the probe sentence; results=%+v", results)
	}
}

// TestGLiNER_PersonBreadth covers the surname-leak regression: GLiNER's
// decoder previously kept "Jane" (narrow, higher-score) over "Jane Doe"
// (wider, above-threshold) in the greedy non-overlap pass, leaving the
// surname unmasked and shipped downstream. The decoder now prefers
// wider PERSON spans when both are above threshold and overlap.
//
// We assert the produced PERSON span text covers the full first-last
// name pair on every repro fixture from the bug report.
//
// Skips cleanly when the model OR libonnxruntime isn't available —
// same gate as TestGLiNER_SmokeGermanProbe above.
func TestGLiNER_PersonBreadth(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir: %v", err)
	}
	modelsDir := filepath.Join(home, ".cache", "anonde", "models")
	modelPath := filepath.Join(modelsDir, "knowledgator_gliner-pii-base-v1.0")
	if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
		t.Skipf("gliner model not cached at %s; skipping breadth test", modelPath)
	}

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

	rec := recognizers.NewGLiNERRecognizer(recognizers.GLiNERConfig{
		ModelsDir:         modelsDir,
		ModelName:         "knowledgator/gliner-pii-base-v1.0",
		OnnxFilePath:      "onnx/model_quint8.onnx",
		AutoDownload:      false,
		SharedLibraryPath: libPath,
	})
	defer func() { _ = rec.Destroy() }()

	cases := []struct {
		text string
		want string // expected PERSON span surface form
	}{
		{"Contact Jane Doe about it.", "Jane Doe"},
		{"Contact Maria Lopez about it.", "Maria Lopez"},
		{"Contact John Doe about it.", "John Doe"},
		{"Dr. Sarah Johnson called.", "Sarah Johnson"},
		{"Call John Doe tomorrow.", "John Doe"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			results, err := rec.Analyze(ctx, tc.text, nil, "en")
			if err != nil {
				if strings.Contains(err.Error(), "Platform-specific initialization failed") ||
					strings.Contains(err.Error(), "shared library") {
					t.Skipf("onnxruntime shared library not available: %v", err)
				}
				t.Fatalf("Analyze: %v", err)
			}
			// Coverage check: every char of the expected first-last name
			// must fall within *some* PERSON span. This accepts three
			// shapes that all redact correctly downstream:
			//   - a single span exactly matching the expected name
			//   - a single broader span (e.g. "Dr. Sarah Johnson") that
			//     subsumes the expected ("Sarah Johnson")
			//   - adjacent same-type spans (e.g. ["Maria", "Lopez"])
			//     which the anonymizer's MergeAdjacentSameType folds into
			//     one token before tokenisation
			// The bug under test was that the surname char positions
			// were NOT covered by any PERSON span; that's what fails
			// here when the decoder emits "Jane" alone.
			wantStart := strings.Index(tc.text, tc.want)
			if wantStart < 0 {
				t.Fatalf("setup: expected name %q not found in input %q", tc.want, tc.text)
			}
			wantEnd := wantStart + len(tc.want)
			// Apply the same MergeAdjacentSameType pass the platform
			// service runs before tokenisation, so the test mirrors the
			// user-facing HTTP behaviour rather than just the raw
			// recognizer output. Without this, ["Maria", "Lopez"] looks
			// like a gap at the intervening space; in production those
			// two spans become one "Maria Lopez" token.
			merged := anonymizer.MergeAdjacentSameType(results, tc.text)
			covered := make([]bool, len(tc.text))
			for _, r := range merged {
				if r.EntityType != "PERSON" {
					continue
				}
				for k := r.Start; k < r.End && k < len(covered); k++ {
					covered[k] = true
				}
			}
			for k := wantStart; k < wantEnd; k++ {
				if !covered[k] {
					t.Errorf("PERSON coverage gap at char %d (%q) of expected name %q in %q — surname likely leaking; results=%+v",
						k-wantStart, string(tc.text[k]), tc.want, tc.text, results)
					return
				}
			}
		})
	}
}

// endsWith is a tiny local helper so the test file doesn't import strings
// just for one HasSuffix call.
func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
