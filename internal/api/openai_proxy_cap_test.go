package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadCapped(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		max     int64
		want    string
		wantErr error
	}{
		{"under cap", "hello", 10, "hello", nil},
		{"at cap", "hello", 5, "hello", nil},
		{"over cap", "hello", 4, "", errResponseTooLarge},
		{"zero cap non-empty", "h", 0, "", errResponseTooLarge},
		{"zero cap empty", "", 0, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readCapped(strings.NewReader(tc.in), tc.max)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// bigCompletion is an upstream that returns a valid but large chat-completion.
func bigCompletion(assistant string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		resp := map[string]any{
			"id":     "chatcmpl-test",
			"object": "chat.completion",
			"model":  "gpt-4o",
			"choices": []map[string]any{{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": assistant},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func newCapProxyServer(t *testing.T, upstreamURL string, maxResp int64) *httptest.Server {
	t.Helper()
	srv := NewHTTPServer(newTestService())
	srv.SetOpenAIProxy(OpenAIProxyConfig{UpstreamBaseURL: upstreamURL, MaxResponseBytes: maxResp})
	client := httptest.NewServer(srv.Routes())
	t.Cleanup(client.Close)
	return client
}

// TestOpenAIProxy_ResponseCapExceeded proves an oversize upstream response is
// rejected with 502 (and the tuning-knob hint) rather than buffered unbounded.
func TestOpenAIProxy_ResponseCapExceeded(t *testing.T) {
	upstream := httptest.NewServer(bigCompletion(strings.Repeat("A", 8192)))
	t.Cleanup(upstream.Close)

	client := newCapProxyServer(t, upstream.URL, 512)
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"Contact john@example.com"}]}`
	resp, err := http.Post(client.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", resp.StatusCode, out)
	}
	if !strings.Contains(string(out), "ANONDE_PROXY_MAX_RESPONSE_BYTES") {
		t.Fatalf("expected cap hint in error body, got %s", out)
	}
}

// TestOpenAIProxy_ResponseUnderCapPasses confirms the cap doesn't false-trigger
// on a normal-sized response: the round-trip still reveals the original PII.
func TestOpenAIProxy_ResponseUnderCapPasses(t *testing.T) {
	// Echo the (anonymized) last message straight back; small body, big cap.
	upstream := httptest.NewServer(http.HandlerFunc(echoLastMessage))
	t.Cleanup(upstream.Close)

	client := newCapProxyServer(t, upstream.URL, 1<<20)
	const email = "john@example.com"
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"Contact ` + email + `"}]}`
	resp, err := http.Post(client.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, out)
	}
	// Reveal must have restored the original email for the client.
	if !strings.Contains(string(out), email) {
		t.Fatalf("expected revealed email in response, got %s", out)
	}
}
