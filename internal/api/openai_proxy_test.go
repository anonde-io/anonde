package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// proxyHarness wires a real Service-backed HTTPServer in front of a
// mock OpenAI upstream. The mock records the body it received (so a
// test can assert no raw PII reached it) and echoes the inbound user
// content straight back as the assistant message; so a correct
// round-trip reveals the original PII to the client.
type proxyHarness struct {
	client      *httptest.Server
	upstream    *httptest.Server
	lastReqBody string
}

func newProxyHarness(t *testing.T, upstreamHandler http.HandlerFunc) *proxyHarness {
	t.Helper()
	h := &proxyHarness{}

	h.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.lastReqBody = string(body)
		// Restore the body so the actual upstream handler can read it.
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		upstreamHandler(w, r)
	}))
	t.Cleanup(h.upstream.Close)

	srv := NewHTTPServer(newTestService())
	srv.SetOpenAIProxy(OpenAIProxyConfig{UpstreamBaseURL: h.upstream.URL})
	h.client = httptest.NewServer(srv.Routes())
	t.Cleanup(h.client.Close)
	return h
}

func (h *proxyHarness) post(t *testing.T, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(h.client.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(out)
}

// echoLastMessage replies with a chat-completion whose assistant
// content is the text of the last message in the request; i.e. it
// echoes back whatever (anonymized) text the proxy forwarded. It
// understands both the string and the multimodal-array content shapes.
func echoLastMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)

	var content string
	if n := len(req.Messages); n > 0 {
		raw := req.Messages[n-1].Content
		if json.Unmarshal(raw, &content) != nil {
			// Multimodal array; concatenate the text parts.
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			_ = json.Unmarshal(raw, &parts)
			for _, p := range parts {
				if p.Type == "text" {
					content += p.Text
				}
			}
		}
	}
	resp := map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"model":   req.Model,
		"choices": []map[string]any{{
			"index":         0,
			"finish_reason": "stop",
			"message": map[string]any{
				"role":    "assistant",
				"content": content,
			},
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func TestOpenAIProxy_RoundTrip(t *testing.T) {
	h := newProxyHarness(t, echoLastMessage)

	// EMAIL_ADDRESS + IP_ADDRESS are both reliably caught by the
	// patterns-only analyzer, so this test doesn't depend on NER.
	const email = "john@example.com"
	const ip = "10.0.0.5"
	reqBody := `{"model":"gpt-4o","messages":[` +
		`{"role":"user","content":"Contact me at ` + email + ` or visit ` + ip + `"}]}`

	resp, out := h.post(t, reqBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, out)
	}

	// The upstream must never have seen the raw PII.
	if strings.Contains(h.lastReqBody, email) {
		t.Errorf("upstream received raw email; body = %s", h.lastReqBody)
	}
	if strings.Contains(h.lastReqBody, ip) {
		t.Errorf("upstream received raw IP; body = %s", h.lastReqBody)
	}
	if !strings.Contains(h.lastReqBody, "EMAIL_ADDRESS_") {
		t.Errorf("upstream did not receive an email token; body = %s", h.lastReqBody)
	}
	// The model field must survive forwarding untouched.
	if !strings.Contains(h.lastReqBody, `"gpt-4o"`) {
		t.Errorf("model field not forwarded; body = %s", h.lastReqBody)
	}

	// The client must get the PII back, de-anonymized.
	var got struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, out)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d; body = %s", len(got.Choices), out)
	}
	content := got.Choices[0].Message.Content
	if !strings.Contains(content, email) || !strings.Contains(content, ip) {
		t.Errorf("response not de-anonymized; content = %q", content)
	}
	if strings.Contains(content, "EMAIL_ADDRESS_") {
		t.Errorf("response still contains an email token; content = %q", content)
	}
}

func TestOpenAIProxy_RejectsStreaming(t *testing.T) {
	h := newProxyHarness(t, echoLastMessage)

	resp, out := h.post(t, `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, out)
	}
	if h.lastReqBody != "" {
		t.Errorf("streaming request should not have reached upstream; body = %s", h.lastReqBody)
	}
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &errResp); err != nil {
		t.Fatalf("decode error response: %v; body = %s", err, out)
	}
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("error type = %q, want invalid_request_error", errResp.Error.Type)
	}
}

func TestOpenAIProxy_MultimodalContent(t *testing.T) {
	h := newProxyHarness(t, echoLastMessage)

	const email = "jane@example.com"
	// Multimodal content: a text part (must be anonymized) and an
	// image_url part (must pass through untouched).
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"my email is ` + email + `"},` +
		`{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}]}]}`

	resp, out := h.post(t, reqBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, out)
	}
	if strings.Contains(h.lastReqBody, email) {
		t.Errorf("upstream received raw email in a text part; body = %s", h.lastReqBody)
	}
	if !strings.Contains(h.lastReqBody, "https://example.com/cat.png") {
		t.Errorf("image_url part was not preserved; body = %s", h.lastReqBody)
	}
	if !strings.Contains(out, email) {
		t.Errorf("response not de-anonymized; body = %s", out)
	}
}

func TestOpenAIProxy_ModelProviderPrefix(t *testing.T) {
	h := newProxyHarness(t, echoLastMessage)

	// An OpenRouter-style "openai/" prefix must be stripped before the
	// request reaches the upstream; OpenAI itself doesn't know the
	// "openai/gpt-4o" id.
	resp, out := h.post(t, `{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi a@b.com"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, out)
	}
	if !strings.Contains(h.lastReqBody, `"model":"gpt-4o"`) {
		t.Errorf("provider prefix not stripped; upstream body = %s", h.lastReqBody)
	}
	if strings.Contains(h.lastReqBody, "openai/gpt-4o") {
		t.Errorf("upstream still saw the provider prefix; body = %s", h.lastReqBody)
	}
}

func TestOpenAIProxy_UnsupportedProvider(t *testing.T) {
	h := newProxyHarness(t, echoLastMessage)

	resp, out := h.post(t, `{"model":"anthropic/claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, out)
	}
	if h.lastReqBody != "" {
		t.Errorf("unsupported-provider request should not have reached upstream; body = %s", h.lastReqBody)
	}
	if !strings.Contains(out, "not supported yet") {
		t.Errorf("expected an unsupported-provider error; body = %s", out)
	}
}

func TestOpenAIProxy_UpstreamErrorPassThrough(t *testing.T) {
	h := newProxyHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	})

	resp, out := h.post(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"contact a@b.com"}]}`)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body = %s", resp.StatusCode, out)
	}
	if !strings.Contains(out, "rate_limit_error") {
		t.Errorf("upstream error not passed through; body = %s", out)
	}
}
