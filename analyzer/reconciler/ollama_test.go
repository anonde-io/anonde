package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anonde-io/anonde/analyzer"
)

func TestParseKeepDrop(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"KEEP", true},
		{"keep", true},
		{" KEEP \n", true},
		{"KEEP — looks like a real name.", true},
		{"YES", true},
		{"yes, it is", true},
		{"DROP", false},
		{"drop", false},
		{"DROP — looks like a lab value", false},
		{"NO", false},
		{"no, this is just a date format", false},
		// Garbled / unsure → fail-open (keep).
		{"", true},
		{"maybe", true},
		{"I'm not sure.", true},
		// Multi-line, first non-empty wins.
		{"\n\nDROP\nrest of output", false},
		{"\n\nKEEP\nbecause it is a person", true},
	}
	for _, tc := range cases {
		if got := parseKeepDrop(tc.in); got != tc.want {
			t.Errorf("parseKeepDrop(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestWindowAround(t *testing.T) {
	t.Parallel()
	text := "0123456789ABCDEFGHIJ"
	if got := windowAround(text, 5, 7, 2); got != "345678" {
		t.Errorf("windowAround mid: got %q, want %q", got, "345678")
	}
	if got := windowAround(text, 0, 2, 5); got != "0123456" {
		t.Errorf("windowAround start: got %q, want %q", got, "0123456")
	}
	if got := windowAround(text, 18, 20, 5); got != "DEFGHIJ" {
		t.Errorf("windowAround end: got %q, want %q", got, "DEFGHIJ")
	}
}

// TestOllamaReconcile_Gating verifies score-band gating without hitting any
// LLM: high-score spans must be kept without a call, low-score dropped
// without a call.
func TestOllamaReconcile_Gating(t *testing.T) {
	t.Parallel()
	var llmCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&llmCalls, 1)
		_ = json.NewEncoder(w).Encode(ollamaResponse{Message: ollamaMessage{Content: "KEEP"}})
	}))
	defer srv.Close()

	rec := NewOllama(OllamaConfig{
		Endpoint:  srv.URL,
		LowGate:   0.40,
		HighGate:  0.85,
		Timeout:   2 * 1e9, // 2s
	})

	text := "irrelevant context " + strings.Repeat("x", 100)
	cand := []analyzer.RecognizerResult{
		{Start: 0, End: 5, Score: 0.95, EntityType: "EMAIL_ADDRESS", RecognizerName: "EmailRecognizer"},   // kept (high)
		{Start: 6, End: 9, Score: 0.20, EntityType: "DATE_TIME", RecognizerName: "DateTimeRecognizer"},    // dropped (low)
		{Start: 10, End: 15, Score: 0.60, EntityType: "PERSON", RecognizerName: "HugotNERRecognizer"},     // LLM called
	}
	got, err := rec.Reconcile(context.Background(), text, cand)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	// Expected: high-score + LLM-keep = 2 kept; low-score dropped.
	if len(got) != 2 {
		t.Errorf("expected 2 kept candidates, got %d: %+v", len(got), got)
	}
	if c := atomic.LoadInt64(&llmCalls); c != 1 {
		t.Errorf("expected exactly 1 LLM call (only the ambiguous span), got %d", c)
	}
}

// TestOllamaReconcile_LLMDrop verifies the reconciler honors a DROP verdict.
func TestOllamaReconcile_LLMDrop(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaResponse{Message: ollamaMessage{Content: "DROP"}})
	}))
	defer srv.Close()

	rec := NewOllama(OllamaConfig{Endpoint: srv.URL, Timeout: 2 * 1e9})

	cand := []analyzer.RecognizerResult{
		{Start: 0, End: 5, Score: 0.55, EntityType: "PERSON"},
	}
	got, err := rec.Reconcile(context.Background(), "hello world here is more text", cand)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 kept (LLM said DROP), got %+v", got)
	}
}

// TestOllamaReconcile_FailOpen verifies that any LLM error keeps the
// candidate — the central anti-leak-rate guarantee.
func TestOllamaReconcile_FailOpen(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	rec := NewOllama(OllamaConfig{Endpoint: srv.URL, Timeout: 2 * 1e9})

	cand := []analyzer.RecognizerResult{
		{Start: 0, End: 5, Score: 0.55, EntityType: "PERSON"},
	}
	got, err := rec.Reconcile(context.Background(), "hello world here is more text", cand)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("fail-open broken: expected 1 kept on LLM error, got %d", len(got))
	}
}

// TestOllamaReconcile_Cache verifies that repeat spans in the same context
// hit the cache instead of the LLM.
func TestOllamaReconcile_Cache(t *testing.T) {
	t.Parallel()
	var llmCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&llmCalls, 1)
		_ = json.NewEncoder(w).Encode(ollamaResponse{Message: ollamaMessage{Content: "KEEP"}})
	}))
	defer srv.Close()

	rec := NewOllama(OllamaConfig{Endpoint: srv.URL, Timeout: 2 * 1e9})

	text := "Bob is here. Bob is here."
	cand := []analyzer.RecognizerResult{
		{Start: 0, End: 3, Score: 0.55, EntityType: "PERSON"},   // "Bob"
		{Start: 13, End: 16, Score: 0.55, EntityType: "PERSON"}, // "Bob" again, different position, same window
	}
	if _, err := rec.Reconcile(context.Background(), text, cand); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	// Both spans live inside overlapping ±200-char windows of the short
	// text — windows are identical, so the cache must collapse to one
	// LLM call.
	if c := atomic.LoadInt64(&llmCalls); c != 1 {
		t.Errorf("cache miss: expected 1 LLM call for two identical spans, got %d", c)
	}
}
