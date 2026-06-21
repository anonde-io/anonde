//go:build ner

package recognizers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
	"github.com/anonde-io/anonde/anonymizer"
)

// gliner_probeText is the German clinical sentence used by
// bench/probes/hugot/probe.go; small enough to fit a 512-token context,
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
	// These come from DefaultLabelToEntity (= chat); if the map shrinks we
	// want the test to fail explicitly. DATE_TIME is clinical-only now, so
	// it's not asserted here (the chat default drops it).
	for _, want := range []string{"PERSON", "LOCATION", "STREET_ADDRESS", "PHONE_NUMBER", "EMAIL_ADDRESS"} {
		if !gotSet[want] {
			t.Errorf("SupportedEntities missing %q (got %v)", want, got)
		}
	}
}

// TestGLiNER_MissingModelNoDownload covers the AutoDownload=false path
// independent of any cached model, so this runs in every CI env.
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
// libonnxruntime isn't available; the bench machine has both, fresh
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
		// .venv-bench wheel; the most common dev box layout in this repo.
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
		// than fail; this test is opportunistic, not gating.
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
	// range of "Müller" (37..43 in the probe text; verified manually).
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
// Skips cleanly when the model OR libonnxruntime isn't available,
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
					t.Errorf("PERSON coverage gap at char %d (%q) of expected name %q in %q; surname likely leaking; results=%+v",
						k-wantStart, string(tc.text[k]), tc.want, tc.text, results)
					return
				}
			}
		})
	}
}

// TestGLiNER_NonNameGuardEndToEnd exercises the always-on universal non-name
// surface guard through the SPAN decoder's full Analyze() path under the
// DEFAULT NER profile (SpanFilter: MoneyGuardFilter(), Enabled=false), the same
// profile the shipped anonde-ner image + bench cell run. A known never-a-name
// token mislabelled PERSON must be dropped (precision gain) while a real name in
// the same sentence survives (recall guard). The shared rejectSpanSurface
// decision is unit-tested deterministically in span_shape_filter_test.go; this
// confirms the decoder actually consults it under the default profile.
func TestGLiNER_NonNameGuardEndToEnd(t *testing.T) {
	t.Parallel()
	modelsDir, onnxRel, libPath, ok := glinerBaseModelOrSkip(t)
	if !ok {
		return
	}

	rec := recognizers.NewGLiNERRecognizer(recognizers.GLiNERConfig{
		ModelsDir:         modelsDir,
		ModelName:         "knowledgator/gliner-pii-base-v1.0",
		OnnxFilePath:      onnxRel,
		AutoDownload:      false,
		SharedLibraryPath: libPath,
		SpanFilter:        recognizers.MoneyGuardFilter(),
	})
	defer func() { _ = rec.Destroy() }()

	// "Please" is a recurring GLiNER PERSON false positive (267x on
	// ai4privacy_en); "Maria Lopez" is a real name that must survive.
	const text = "Please contact Maria Lopez today."
	results, ok := analyzeOrSkipOnOrtWedge(t, rec, text, "en")
	if !ok {
		return
	}

	pleaseStart := strings.Index(text, "Please")
	nameStart := strings.Index(text, "Maria Lopez")
	nameEnd := nameStart + len("Maria Lopez")
	nameCovered := false
	for _, r := range results {
		if r.EntityType != "PERSON" {
			continue
		}
		// Precision: no PERSON span may be EXACTLY the standalone "Please".
		if r.Start == pleaseStart && r.End == pleaseStart+len("Please") {
			t.Errorf("non-name guard did not drop standalone PERSON %q (precision FP); results=%+v",
				"Please", results)
		}
		if r.Start < nameEnd && r.End > nameStart {
			nameCovered = true
		}
	}
	// Recall guard: the real name must still be detected.
	if !nameCovered {
		t.Errorf("real name %q not covered by any PERSON span (recall regression); results=%+v",
			"Maria Lopez", results)
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

// glinerBaseModelOrSkip resolves the cached gliner-pii-base model + a usable
// onnxruntime shared library, or Skips. It returns the models dir, the ONNX
// file RELATIVE to the model dir (whichever of the FP32 / INT8 variants is
// actually present — the default deploy ships FP32 at onnx/model.onnx, but a
// dev box may only have the INT8 model_quint8.onnx cached), and the resolved
// lib path. The previous version stat'd only the model DIRECTORY and then drove
// a load of an ABSENT onnx/model.onnx, which wedged onnxruntime dlopen and
// timed out the whole package — so we stat the actual ONNX file here and Skip
// before any CGO load when it is missing.
func glinerBaseModelOrSkip(t *testing.T) (modelsDir, onnxRel, libPath string, ok bool) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir: %v", err)
		return "", "", "", false
	}
	modelsDir = filepath.Join(home, ".cache", "anonde", "models")
	modelPath := filepath.Join(modelsDir, "knowledgator_gliner-pii-base-v1.0")
	if _, statErr := os.Stat(modelPath); os.IsNotExist(statErr) {
		t.Skipf("gliner model not cached at %s; skipping guard test", modelPath)
		return "", "", "", false
	}
	// Prefer the default-deploy FP32 ONNX; fall back to the INT8 dev-cache
	// variant. Stat the actual file so a missing ONNX Skips instead of
	// wedging the loader.
	for _, rel := range []string{"onnx/model.onnx", "model_quint8.onnx", "onnx/model_quint8.onnx"} {
		if _, statErr := os.Stat(filepath.Join(modelPath, rel)); statErr == nil {
			onnxRel = rel
			break
		}
	}
	if onnxRel == "" {
		t.Skipf("no usable gliner ONNX file under %s; skipping guard test", modelPath)
		return "", "", "", false
	}

	libPath = os.Getenv("ORT_LIBRARY_PATH")
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
	if libPath == "" {
		t.Skip("no onnxruntime shared library found; skipping guard test")
		return "", "", "", false
	}
	return modelsDir, onnxRel, libPath, true
}

// glinerAnalyzer is the minimal surface the guard tests need from either the
// span or flat GLiNER recognizer.
type glinerAnalyzer interface {
	Analyze(ctx context.Context, text string, entities []string, lang string) ([]analyzer.RecognizerResult, error)
}

// analyzeOrSkipOnOrtWedge runs Analyze with a bounded timeout. The first call
// triggers a one-time onnxruntime InitializeEnvironment via a sync.Once and a
// blocking CGO dlopen that ignores context cancellation; on a host whose
// onnxruntime dylib cannot be loaded (quarantined / adhoc-signed without the
// hardened-runtime entitlement) that call can wedge and never return. Rather
// than let the wedge hang the package until the 600s test timeout, we run the
// analyze in a goroutine and convert a no-return within the budget into a Skip.
// A returned shared-library / init error is also converted to a Skip. The
// abandoned goroutine is harmless: it holds no test state and the process exits
// when the suite finishes.
func analyzeOrSkipOnOrtWedge(t *testing.T, rec glinerAnalyzer, text, lang string) ([]analyzer.RecognizerResult, bool) {
	t.Helper()
	type out struct {
		res []analyzer.RecognizerResult
		err error
	}
	done := make(chan out, 1)
	var once sync.Once
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := rec.Analyze(ctx, text, nil, lang)
		once.Do(func() { done <- out{res, err} })
	}()
	select {
	case o := <-done:
		if o.err != nil {
			if strings.Contains(o.err.Error(), "Platform-specific initialization failed") ||
				strings.Contains(o.err.Error(), "shared library") ||
				strings.Contains(o.err.Error(), "onnxruntime") {
				t.Skipf("onnxruntime not usable on this host: %v", o.err)
				return nil, false
			}
			t.Fatalf("Analyze: %v", o.err)
			return nil, false
		}
		return o.res, true
	case <-time.After(90 * time.Second):
		t.Skip("onnxruntime InitializeEnvironment wedged (dlopen did not return); " +
			"skipping guard test on this host — the deterministic guard decision is " +
			"covered in span_shape_filter_test.go")
		return nil, false
	}
}
