// Command anonde-hook is a Claude Code hook that detects PII in prompts and
// tool calls before they reach the model, using anonde for detection.
//
// It is invoked by Claude Code with a hook-event JSON payload on stdin and
// communicates its decision via stdout JSON + exit code (see
// https://code.claude.com/docs/en/hooks). Two events are handled:
//
//   - UserPromptSubmit: scans the prompt the developer just submitted. The
//     hook contract does not allow rewriting a prompt, so the hook can only
//     warn (default) or block.
//   - PreToolUse: scans the text a tool is about to act on (the Bash command,
//     the file content of a Write/Edit, etc.) — i.e. PII the agent is about
//     to embed in an action that could leave the machine (a curl, a commit, a
//     log line). The hook warns (default) or denies.
//
// Detection runs IN-PROCESS by default (pure-Go pattern recognizers, no
// server, no network) so the hook works the moment it is installed. Set
// ANONDE_HOOK_URL to point at a running anonde server to get full GLiNER NER
// (names, places, orgs) instead of patterns-only.
//
// The hook is designed to fail open: any malformed payload, detector error, or
// unreachable server results in "allow" so a misconfiguration can never brick a
// Claude Code session. Set ANONDE_HOOK_FAIL_OPEN=false to flip that for
// high-security setups.
//
// Configuration (all optional, via environment):
//
//	ANONDE_HOOK_MODE       off | warn | block        (default: warn)
//	ANONDE_HOOK_URL        e.g. http://localhost:8081 (unset: in-process patterns)
//	ANONDE_HOOK_TENANT     vault tenant id            (default: claude-code-hook)
//	ANONDE_HOOK_LANGUAGE   recognizer language        (default: en)
//	ANONDE_HOOK_MIN_SCORE  drop findings below score  (default: 0.40)
//	ANONDE_HOOK_ENTITIES   comma list to restrict to  (default: all)
//	ANONDE_HOOK_TIMEOUT_MS server call timeout in ms  (default: 1500)
//	ANONDE_HOOK_FAIL_OPEN  true | false               (default: true)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
)

// version is stamped at release time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

// mode is the hook's enforcement posture.
type mode string

const (
	modeOff   mode = "off"
	modeWarn  mode = "warn"
	modeBlock mode = "block"
)

// config is resolved once from the environment.
type config struct {
	mode      mode
	serverURL string // empty => in-process detection
	tenant    string
	language  string
	minScore  float64
	entities  map[string]bool // empty => all entity types count
	timeout   time.Duration
	failOpen  bool
}

func loadConfig() config {
	c := config{
		mode:     modeWarn,
		tenant:   "claude-code-hook",
		language: "en",
		minScore: 0.40,
		timeout:  1500 * time.Millisecond,
		failOpen: true,
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ANONDE_HOOK_MODE"))); v != "" {
		c.mode = mode(v)
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_URL")); v != "" {
		c.serverURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_TENANT")); v != "" {
		c.tenant = v
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_LANGUAGE")); v != "" {
		c.language = v
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_MIN_SCORE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.minScore = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_ENTITIES")); v != "" {
		c.entities = map[string]bool{}
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				c.entities[strings.ToUpper(e)] = true
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			c.timeout = time.Duration(ms) * time.Millisecond
		}
	}
	if v := strings.TrimSpace(os.Getenv("ANONDE_HOOK_FAIL_OPEN")); v != "" {
		c.failOpen, _ = strconv.ParseBool(v)
	}
	return c
}

// hookInput is the subset of the Claude Code hook payload we read.
type hookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Prompt        string          `json:"prompt"`
}

// finding is a normalized detection from either detection backend.
type finding struct {
	EntityType string
	Start, End int
	Score      float64
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Fprintln(os.Stdout, "anonde-hook "+version)
			return
		case "--help", "-h", "help":
			fmt.Fprintln(os.Stdout, "anonde-hook — Claude Code PII-guard hook. Reads a hook-event JSON\n"+
				"payload on stdin and emits a decision. Configure via ANONDE_HOOK_* env\n"+
				"vars; see https://github.com/anonde-io/anonde/tree/main/plugins/claude-code")
			return
		}
	}
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

// run reads the hook payload, decides, writes the response JSON, and returns
// the process exit code. It never returns a non-zero "block via exit code":
// blocking is expressed in the JSON body, and exit 0 keeps the contract simple
// and the failure modes safe.
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	cfg := loadConfig()

	raw, err := io.ReadAll(io.LimitReader(stdin, 8<<20)) // 8 MiB guard
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return 0 // nothing to inspect
	}

	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Fprintf(stderr, "anonde-hook: malformed payload: %v\n", err)
		return 0 // fail open: never break the session on a parse error
	}

	if cfg.mode == modeOff {
		return 0
	}

	text, ok := scanTarget(in)
	if !ok || strings.TrimSpace(text) == "" {
		return 0 // this event/tool carries nothing worth scanning
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	findings, err := detect(ctx, cfg, text)
	if err != nil {
		fmt.Fprintf(stderr, "anonde-hook: detection error: %v\n", err)
		if cfg.failOpen {
			return 0
		}
		// fail closed: treat an unreachable detector as "assume PII present".
		return emitUnavailable(cfg, in, stdout)
	}

	findings = filter(cfg, findings)
	if len(findings) == 0 {
		return 0 // clean
	}

	switch in.HookEventName {
	case "UserPromptSubmit":
		return emitPrompt(cfg, findings, stdout)
	default: // PreToolUse (and any future tool-bearing event)
		return emitPreTool(cfg, in.ToolName, findings, stdout)
	}
}

// scanTarget returns the text to inspect for the given event, plus whether this
// event is one the hook acts on at all.
func scanTarget(in hookInput) (string, bool) {
	switch in.HookEventName {
	case "UserPromptSubmit":
		return in.Prompt, true
	case "PreToolUse":
		return toolText(in.ToolName, in.ToolInput)
	default:
		return "", false
	}
}

// toolText extracts the PII-bearing free text from a tool's input. Only tools
// whose input embeds developer/agent-authored content are scanned; navigation
// tools (Read, Glob, Grep, …) carry no payload to leak and are skipped.
func toolText(tool string, rawInput json.RawMessage) (string, bool) {
	var ti map[string]any
	if err := json.Unmarshal(rawInput, &ti); err != nil {
		return "", false
	}
	str := func(k string) string {
		if s, ok := ti[k].(string); ok {
			return s
		}
		return ""
	}
	switch tool {
	case "Bash":
		return str("command"), true
	case "Write":
		return str("content"), true
	case "Edit":
		// new_string is what gets written; old_string is matched against the
		// existing file. Scan both so PII isn't smuggled in via either.
		return str("old_string") + "\n" + str("new_string"), true
	case "MultiEdit":
		var b strings.Builder
		if edits, ok := ti["edits"].([]any); ok {
			for _, e := range edits {
				if m, ok := e.(map[string]any); ok {
					if s, ok := m["old_string"].(string); ok {
						b.WriteString(s + "\n")
					}
					if s, ok := m["new_string"].(string); ok {
						b.WriteString(s + "\n")
					}
				}
			}
		}
		return b.String(), true
	case "NotebookEdit":
		return str("new_source"), true
	default:
		return "", false
	}
}

// detect runs the configured detection backend over text.
func detect(ctx context.Context, cfg config, text string) ([]finding, error) {
	if cfg.serverURL != "" {
		return detectServer(ctx, cfg, text)
	}
	return detectInProcess(ctx, cfg, text)
}

// engine is lazily built once; DefaultAnalyzerEngine wires the pure-Go pattern
// recognizers (no CGO, no model, no network).
var engine *analyzer.AnalyzerEngine

func detectInProcess(ctx context.Context, cfg config, text string) ([]finding, error) {
	if engine == nil {
		engine = anonde.DefaultAnalyzerEngine()
	}
	results, err := engine.Analyze(ctx, text, analyzer.AnalysisConfig{
		Language:        cfg.language,
		ScoreThreshold:  cfg.minScore,
		RemoveConflicts: true,
		DisableNER:      true, // in-process path is patterns-only by construction
	})
	if err != nil {
		return nil, err
	}
	out := make([]finding, 0, len(results))
	for _, r := range results {
		out = append(out, finding{EntityType: r.EntityType, Start: r.Start, End: r.End, Score: r.Score})
	}
	return out, nil
}

func detectServer(ctx context.Context, cfg config, text string) ([]finding, error) {
	body, _ := json.Marshal(map[string]any{
		"tenant_id":      cfg.tenant,
		"content":        text,
		"content_format": "text",
		"options": map[string]any{
			"language": cfg.language,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.serverURL+"/v1/anonymizations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("anonde server %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Findings []struct {
			EntityType string  `json:"entity_type"`
			Start      int     `json:"start"`
			End        int     `json:"end"`
			Score      float64 `json:"score"`
		} `json:"findings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := make([]finding, 0, len(out.Findings))
	for _, f := range out.Findings {
		res = append(res, finding{EntityType: f.EntityType, Start: f.Start, End: f.End, Score: f.Score})
	}
	return res, nil
}

// filter applies the min-score floor and the optional entity allow-list.
func filter(cfg config, in []finding) []finding {
	out := in[:0:0]
	for _, f := range in {
		if f.Score < cfg.minScore {
			continue
		}
		if len(cfg.entities) > 0 && !cfg.entities[strings.ToUpper(f.EntityType)] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// summary renders findings as a stable, human-readable count, e.g.
// "EMAIL_ADDRESS×2, PERSON×1".
func summary(findings []finding) string {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.EntityType]++
	}
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool {
		if counts[types[i]] != counts[types[j]] {
			return counts[types[i]] > counts[types[j]]
		}
		return types[i] < types[j]
	})
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, fmt.Sprintf("%s×%d", t, counts[t]))
	}
	return strings.Join(parts, ", ")
}

// --- Output payloads (Claude Code hook response schema) ---

type preToolOut struct {
	HookSpecificOutput preToolHSO `json:"hookSpecificOutput"`
	SystemMessage      string     `json:"systemMessage,omitempty"`
}

type preToolHSO struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

type promptOut struct {
	Decision           string     `json:"decision,omitempty"`
	Reason             string     `json:"reason,omitempty"`
	HookSpecificOutput *promptHSO `json:"hookSpecificOutput,omitempty"`
	SystemMessage      string     `json:"systemMessage,omitempty"`
}

type promptHSO struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

func emitPreTool(cfg config, tool string, findings []finding, stdout io.Writer) int {
	sum := summary(findings)
	out := preToolOut{HookSpecificOutput: preToolHSO{HookEventName: "PreToolUse"}}
	switch cfg.mode {
	case modeBlock:
		out.HookSpecificOutput.PermissionDecision = "deny"
		out.HookSpecificOutput.PermissionDecisionReason = fmt.Sprintf(
			"anonde blocked this %s call: it contains PII (%s). Remove or tokenize the sensitive data, or set ANONDE_HOOK_MODE=warn to allow with a warning.",
			tool, sum)
		out.SystemMessage = fmt.Sprintf("🛡️  anonde blocked a %s call carrying PII (%s)", tool, sum)
	default: // warn
		out.HookSpecificOutput.AdditionalContext = fmt.Sprintf(
			"anonde detected PII in this %s call (%s). The data will be sent as-is; redact it if it should not leave this machine.",
			tool, sum)
		out.SystemMessage = fmt.Sprintf("⚠️  anonde: PII in %s call (%s)", tool, sum)
	}
	writeJSON(stdout, out)
	return 0
}

func emitPrompt(cfg config, findings []finding, stdout io.Writer) int {
	sum := summary(findings)
	out := promptOut{}
	switch cfg.mode {
	case modeBlock:
		out.Decision = "block"
		out.Reason = fmt.Sprintf(
			"anonde blocked this prompt: it contains PII (%s). Re-submit without the sensitive data, or set ANONDE_HOOK_MODE=warn to allow with a warning.",
			sum)
		out.SystemMessage = fmt.Sprintf("🛡️  anonde blocked a prompt carrying PII (%s)", sum)
	default: // warn — prompts cannot be rewritten, so we surface, not silence
		out.HookSpecificOutput = &promptHSO{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: fmt.Sprintf("anonde detected PII in this prompt (%s); it is being sent to the model.", sum),
		}
		out.SystemMessage = fmt.Sprintf("⚠️  anonde: PII in prompt (%s)", sum)
	}
	writeJSON(stdout, out)
	return 0
}

// emitUnavailable is the fail-closed response when detection could not run.
func emitUnavailable(cfg config, in hookInput, stdout io.Writer) int {
	const reason = "anonde could not scan for PII (detector unavailable) and is configured fail-closed (ANONDE_HOOK_FAIL_OPEN=false)."
	if in.HookEventName == "UserPromptSubmit" {
		writeJSON(stdout, promptOut{Decision: "block", Reason: reason, SystemMessage: "🛡️  " + reason})
		return 0
	}
	writeJSON(stdout, preToolOut{
		HookSpecificOutput: preToolHSO{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		},
		SystemMessage: "🛡️  " + reason,
	})
	return 0
}

func writeJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	w.Write(b)
	io.WriteString(w, "\n")
}
