//go:build hugot

// gliner_session_options.go centralises the construction of an
// *ort.SessionOptions from environment variables. Both ner_gliner.go
// and ner_gliner_flat.go call sessionOptionsFromEnv() in their init()
// so that the three ORT knobs ship from one place and both decoders
// pick them up automatically.
//
// CONTRACT: callers MUST invoke initOrtEnvironment() FIRST. ORT's
// NewSessionOptions() requires IsInitialized() to be true, and will
// otherwise return NotInitializedError. The init() flow in both
// recognizers already does this; keep that order if you ever reorder
// init blocks.
//
// Returns (nil, nil) when no env knob is set, so the caller can pass
// the result straight to NewDynamicAdvancedSession (which accepts nil
// for "use ORT defaults"). This preserves byte-identical behaviour
// for users that don't opt in to tuning.

package recognizers

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

// sessionOptionsFromEnv returns a configured *ort.SessionOptions if any
// of ANONDE_ORT_INTRA_OP_THREADS / ANONDE_ORT_INTER_OP_THREADS /
// ANONDE_ORT_GRAPH_OPT_LEVEL is set, otherwise (nil, nil).
//
// Bad env values are LOGGED and ignored (the knob falls back to ORT's
// default). Same precedent as GLINER_THRESHOLD and GLINER_POOL_SIZE in
// cmd/anonde/main.go; a typo in a tuning knob must never keep the
// server from booting.
//
// On any unrecoverable ORT error (e.g. NewSessionOptions itself failing,
// which means the runtime is broken), returns (nil, err) so the caller
// can decide whether to fall through to defaults or fatal out.
func sessionOptionsFromEnv() (*ort.SessionOptions, error) {
	intraRaw := strings.TrimSpace(os.Getenv("ANONDE_ORT_INTRA_OP_THREADS"))
	interRaw := strings.TrimSpace(os.Getenv("ANONDE_ORT_INTER_OP_THREADS"))
	graphRaw := strings.TrimSpace(os.Getenv("ANONDE_ORT_GRAPH_OPT_LEVEL"))

	// Fast path: nothing configured, hand back nil so the caller uses
	// the ORT defaults exactly as before.
	if intraRaw == "" && interRaw == "" && graphRaw == "" {
		return nil, nil
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("gliner: NewSessionOptions: %w", err)
	}

	intra := parsePositiveInt("ANONDE_ORT_INTRA_OP_THREADS", intraRaw)
	if intra > 0 {
		if e := opts.SetIntraOpNumThreads(intra); e != nil {
			log.Printf("gliner: SetIntraOpNumThreads(%d) failed: %v (falling back to default)", intra, e)
		}
	}

	inter := parsePositiveInt("ANONDE_ORT_INTER_OP_THREADS", interRaw)
	if inter > 0 {
		if e := opts.SetInterOpNumThreads(inter); e != nil {
			log.Printf("gliner: SetInterOpNumThreads(%d) failed: %v (falling back to default)", inter, e)
		}
	}

	graphLevel, graphLabel, graphOK := parseGraphOptLevel(graphRaw)
	if graphOK {
		if e := opts.SetGraphOptimizationLevel(graphLevel); e != nil {
			log.Printf("gliner: SetGraphOptimizationLevel(%s) failed: %v (falling back to default)", graphLabel, e)
		}
	}

	logSessionOpts(intra, inter, graphLabel)
	return opts, nil
}

// parsePositiveInt parses a positive integer from an env var. Returns 0
// (use ORT default) when raw is empty, malformed, or <= 0. Malformed
// values are logged for operator visibility.
func parsePositiveInt(key, raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("%s=%q ignored: %v (using ORT default)", key, raw, err)
		return 0
	}
	if n < 1 {
		log.Printf("%s=%d ignored: must be >= 1 (using ORT default)", key, n)
		return 0
	}
	return n
}

// parseGraphOptLevel maps the ANONDE_ORT_GRAPH_OPT_LEVEL env string to
// yalue/onnxruntime_go's GraphOptimizationLevel constants. Returns
// (level, label, true) on success or (_, _, false) when raw is empty or
// not recognised (the caller skips the SetGraphOptimizationLevel call,
// keeping ORT's default of "basic"). Unrecognised values are logged.
func parseGraphOptLevel(raw string) (ort.GraphOptimizationLevel, string, bool) {
	if raw == "" {
		return 0, "", false
	}
	switch strings.ToLower(raw) {
	case "disabled", "disable_all", "off", "none":
		return ort.GraphOptimizationLevelDisableAll, "disabled", true
	case "basic", "enable_basic":
		return ort.GraphOptimizationLevelEnableBasic, "basic", true
	case "extended", "enable_extended":
		return ort.GraphOptimizationLevelEnableExtended, "extended", true
	case "all", "enable_all":
		return ort.GraphOptimizationLevelEnableAll, "all", true
	default:
		log.Printf("ANONDE_ORT_GRAPH_OPT_LEVEL=%q ignored (valid: disabled, basic, extended, all); using ORT default", raw)
		return 0, "", false
	}
}

// logSessionOpts emits one summary line describing which knobs are in
// effect, so operators can confirm tuning landed. 0 means "ORT default"
// for the integer knobs; "default" for the graph-opt label.
func logSessionOpts(intra, inter int, graph string) {
	intraStr := "default"
	if intra > 0 {
		intraStr = strconv.Itoa(intra)
	}
	interStr := "default"
	if inter > 0 {
		interStr = strconv.Itoa(inter)
	}
	graphStr := graph
	if graphStr == "" {
		graphStr = "default"
	}
	log.Printf("gliner: session opts intra=%s inter=%s graph=%s", intraStr, interStr, graphStr)
}
