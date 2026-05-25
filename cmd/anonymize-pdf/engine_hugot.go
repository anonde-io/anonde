//go:build hugot

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

// buildAnalyzerEngine selects the analyzer backend. With `-tags hugot`
// the NER paths (gliner / hugot) are wired; otherwise the patterns-only
// fallback returns from engine_default.go.
//
// Env knobs honored (subset of the server's analyzerFromEnv) so a CLI
// invocation and the HTTP server produce comparable results when run
// against the same model:
//
//   - GLINER_MODEL      (default: knowledgator/gliner-pii-base-v1.0)
//   - GLINER_ONNX_FILE  (default: onnx/model.onnx — FP32, matches prod)
//   - GLINER_THRESHOLD  (default: 0 = recognizer default 0.40)
//   - GLINER_MODELS_DIR / ORT_SO_PATH
//
// The CLI auto-downloads the model on first use so a stranger's laptop
// can `go run` it without any setup beyond `brew install onnxruntime`.
func buildAnalyzerEngine(backend string) (*analyzer.AnalyzerEngine, string, error) {
	switch backend {
	case "", "auto", "gliner":
		// auto = gliner when hugot-tagged build is in use, since
		// libonnxruntime is the only extra runtime dep and it's
		// already required to even *load* this code path.
		ensureORTPath()
		modelName := envDefault("GLINER_MODEL", "knowledgator/gliner-pii-base-v1.0")
		onnxPath := envDefault("GLINER_ONNX_FILE", "onnx/model.onnx")
		// Default threshold is intentionally lower than the server's
		// 0.40 — forms/contracts/legal-letter PDFs have dense PII
		// and the cost of over-redaction is low compared to leaking,
		// so trade some precision for higher recall by default.
		// Override per-run with GLINER_THRESHOLD.
		threshold := 0.3
		if raw := strings.TrimSpace(os.Getenv("GLINER_THRESHOLD")); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				threshold = v
			}
		}
		// Extend the default label set with entity types that the
		// "match Private AI" PDF flow specifically cares about but the
		// shared default omits: durations, times of day, structured
		// numeric refs.
		//
		// Deliberately NOT included: "money" / "monetary amount" —
		// they fire on every numeric column value in a fine /
		// invoice table (250,00 / 750,00 / 4.695,00 rows in a
		// garnishment notice), but those per-line amounts are public
		// penalty values and not PII. The Romanian money pattern
		// (with required currency suffix — lei / RON / EUR) catches
		// the genuinely PII cases (totals owed, amounts in narrative
		// prose) without the table-noise.
		labels := append([]string(nil), recognizers.DefaultPIILabels...)
		labels = append(labels,
			"duration", "time period",
			"time", "time of day",
			"vehicle registration", "license plate",
			"file number", "case number", "reference number",
		)
		labelMap := map[string]string{}
		for k, v := range recognizers.DefaultLabelToEntity {
			labelMap[k] = v
		}
		labelMap["duration"] = "DURATION"
		labelMap["time period"] = "DURATION"
		labelMap["time"] = "TIME"
		labelMap["time of day"] = "TIME"
		labelMap["vehicle registration"] = "ID"
		labelMap["license plate"] = "ID"
		labelMap["file number"] = "ID"
		labelMap["case number"] = "ID"
		labelMap["reference number"] = "ID"
		return anonde.DefaultAnalyzerEngineWithGLiNERConfig(recognizers.GLiNERConfig{
			ModelsDir:         os.Getenv("GLINER_MODELS_DIR"),
			ModelName:         modelName,
			OnnxFilePath:      onnxPath,
			AutoDownload:      true,
			SharedLibraryPath: os.Getenv("ORT_SO_PATH"),
			Threshold:         threshold,
			Labels:            labels,
			LabelToEntity:     labelMap,
		}), fmt.Sprintf("gliner:%s", modelName), nil
	case "patterns", "patterns-only":
		return anonde.DefaultAnalyzerEngine(), "patterns", nil
	case "hugot":
		modelName := envDefault("HUGOT_MODEL", "Isotonic/distilbert_finetuned_ai4privacy_v2")
		return anonde.DefaultAnalyzerEngineWithHugot(
			os.Getenv("HUGOT_MODELS_DIR"),
			modelName,
			true,
		), "hugot:" + modelName, nil
	default:
		return nil, "", fmt.Errorf("unknown backend %q", backend)
	}
}

func envDefault(k, dflt string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return dflt
}

// ensureORTPath sets ORT_SO_PATH to a sensible default for the host
// platform if the user hasn't already chosen one. macOS Homebrew users
// land at /opt/homebrew/lib/libonnxruntime.dylib by default; Linux
// distros vary. yalue/onnxruntime_go fails fast with a clear error if
// the path is wrong, so a wrong guess here is loud, not silent.
func ensureORTPath() {
	if os.Getenv("ORT_SO_PATH") != "" {
		return
	}
	candidates := []string{
		"/opt/homebrew/lib/libonnxruntime.dylib", // macOS arm64 Homebrew
		"/usr/local/lib/libonnxruntime.dylib",    // macOS Intel Homebrew
		"/usr/local/lib/libonnxruntime.so",       // Linux installs
		"/usr/lib/libonnxruntime.so",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			os.Setenv("ORT_SO_PATH", p)
			return
		}
	}
}
