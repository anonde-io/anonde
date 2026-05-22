package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anonde-io/anonde/internal/core"
)

// OpenAI-compatible proxy: a client points its existing OpenAI SDK at
// anonde's base URL instead of api.openai.com. anonde anonymizes the
// prompt, forwards to the configured upstream provider, de-anonymizes
// the response and returns it in OpenAI shape. No plugin, no client
// code change beyond the base URL.
//
// The route is a single OpenAI-shaped endpoint, POST /v1/chat/completions,
// so the client base URL is byte-identical to a real OpenAI swap
// (http://host/v1). The upstream provider is selected in-band — the
// OpenRouter convention — by a "provider/model" prefix on the `model`
// field: "openai/gpt-4o" routes to OpenAI and forwards the bare model
// "gpt-4o" upstream. A model with no prefix defaults to OpenAI. v0.1
// proxies OpenAI only; any other provider prefix is rejected with a
// clear error. Anthropic / Gemini routing lands in v0.2.
//
// v0.1 is non-streaming only — a `stream: true` request is rejected
// rather than silently downgraded, because SSE de-anonymization needs
// a placeholder re-assembler (a `<PERSON_1>` token can split across
// two chunks). Streaming lands in v0.1.1; see launch_plan.md.

const (
	// chatCompletionsPath is the proxy route (method + path). The
	// method+path pattern is strictly more specific than the "/v1/"
	// subtree the REST gateway owns, so ServeMux routes it cleanly.
	chatCompletionsPath = "POST /v1/chat/completions"

	// defaultProvider is assumed when the `model` field carries no
	// "provider/" prefix.
	defaultProvider = "openai"

	defaultOpenAIBaseURL   = "https://api.openai.com/v1"
	defaultProxyTenant     = "openai-proxy"
	defaultUpstreamTimeout = 120 * time.Second

	// proxyActor / proxyPurpose are the audit identity recorded on the
	// reveal call. The proxy round-trip is system-initiated, so these
	// are fixed rather than caller-supplied.
	proxyActor   = "openai-proxy"
	proxyPurpose = "chat-completion-roundtrip"
)

// OpenAIProxyConfig configures the OpenAI upstream the proxy forwards
// to (POST /v1/chat/completions, model ids prefixed "openai/"). It is
// per-provider by design — a future AnthropicProxyConfig will be its
// sibling, selected by the "anthropic/" model prefix. Every field has safe
// zero-value behaviour: an empty UpstreamBaseURL defaults to OpenAI, an
// empty UpstreamAPIKey forwards no Authorization header (which is what
// a local Ollama wants), an empty DefaultTenant falls back to
// "openai-proxy", and a nil HTTPClient gets a default client with a
// 120s timeout.
type OpenAIProxyConfig struct {
	// UpstreamBaseURL is the OpenAI-compatible API root, e.g.
	// "https://api.openai.com/v1" or "http://localhost:11434/v1" for a
	// local Ollama. The proxy POSTs to <base>/chat/completions.
	UpstreamBaseURL string
	// UpstreamAPIKey is forwarded as "Authorization: Bearer <key>".
	// Empty means no Authorization header is sent.
	UpstreamAPIKey string
	// DefaultTenant scopes the vault when a request carries no
	// X-Anonde-Tenant header.
	DefaultTenant string
	// RequestTimeout bounds the upstream call. Ignored when HTTPClient
	// is supplied.
	RequestTimeout time.Duration
	// HTTPClient lets callers (and tests) inject a client. Nil means a
	// default client with RequestTimeout (or 120s) is built.
	HTTPClient *http.Client
}

// openAIProxy holds the resolved proxy configuration and the core
// Service it delegates anonymize / reveal to. It does not duplicate
// any orchestration — anonymizeSegments calls Service.Ingest and
// revealResponse calls Service.Reveal.
type openAIProxy struct {
	svc    *core.Service
	cfg    OpenAIProxyConfig
	client *http.Client
}

func newOpenAIProxy(svc *core.Service, cfg OpenAIProxyConfig) *openAIProxy {
	if strings.TrimSpace(cfg.UpstreamBaseURL) == "" {
		cfg.UpstreamBaseURL = defaultOpenAIBaseURL
	}
	cfg.UpstreamBaseURL = strings.TrimRight(strings.TrimSpace(cfg.UpstreamBaseURL), "/")
	if strings.TrimSpace(cfg.DefaultTenant) == "" {
		cfg.DefaultTenant = defaultProxyTenant
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.RequestTimeout
		if timeout <= 0 {
			timeout = defaultUpstreamTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	return &openAIProxy{svc: svc, cfg: cfg, client: client}
}

// chatCompletions implements POST /v1/chat/completions.
//
// The request body is parsed loosely (every field kept as a
// json.RawMessage) so model, temperature, tools and any other field we
// don't care about are forwarded byte-for-byte. Only messages[].content
// is touched on the way in, and only choices[].message.content on the
// way out.
func (p *openAIProxy) chatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		// http.MaxBytesReader (wired in Routes) surfaces an oversize
		// body here; 413 is the honest status for that case.
		writeOpenAIError(w, http.StatusRequestEntityTooLarge, "invalid_request_error",
			"read request body: "+err.Error())
		return
	}

	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
			"request body is not a valid JSON object")
		return
	}

	if rawBool(req["stream"]) {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
			"streaming responses are not supported yet — set stream:false "+
				"(anonde v0.1 limitation; streaming lands in v0.1.1)")
		return
	}

	// Provider routing: an OpenRouter-style "provider/model" prefix on
	// the model field selects the upstream. v0.1 proxies OpenAI only,
	// so anything else is a clear 400 rather than a surprise upstream
	// failure. The prefix is stripped before forwarding so the upstream
	// sees its own bare model id.
	provider, bareModel, hadPrefix := parseModelProvider(req["model"])
	if provider != defaultProvider {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("model provider %q is not supported yet — anonde v0.1 proxies "+
				"%q only; use a %q-prefixed model id (e.g. %q/gpt-4o) or none",
				provider, defaultProvider, defaultProvider, defaultProvider))
		return
	}
	if hadPrefix {
		req["model"], _ = json.Marshal(bareModel)
	}

	tenant := strings.TrimSpace(r.Header.Get("X-Anonde-Tenant"))
	if tenant == "" {
		tenant = p.cfg.DefaultTenant
	}

	var messages []map[string]json.RawMessage
	if raw, ok := req["messages"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &messages); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
				"messages is not a valid array")
			return
		}
	}

	// One job per anonymizable text segment across all messages.
	var jobs []*segJob
	for _, msg := range messages {
		jobs = append(jobs, collectSegmentJobs(msg)...)
	}

	var anonID string
	if len(jobs) > 0 {
		segs := make([]string, len(jobs))
		for i, j := range jobs {
			segs[i] = j.text
		}
		anonymized, id, err := p.anonymizeSegments(ctx, tenant, segs)
		if err != nil {
			log.Printf("openai-proxy: anonymize failed: %v", err)
			writeOpenAIError(w, http.StatusBadGateway, "anonde_error",
				"anonymize prompt: "+err.Error())
			return
		}
		anonID = id
		for i, j := range jobs {
			j.set(anonymized[i])
		}
		// Re-marshal the (mutated) message maps back into the request.
		req["messages"], _ = json.Marshal(messages)
	}

	// The vault entries only need to outlive this round-trip. Delete
	// them once the response is revealed so a busy proxy doesn't grow
	// the vault unboundedly; ANONDE_VAULT_TTL remains the backstop if
	// the process dies mid-request. context.Background() because the
	// request context is already cancelled by the time defers run.
	if anonID != "" {
		defer func() {
			if _, err := p.svc.DeleteAnonymization(context.Background(), tenant, anonID); err != nil {
				log.Printf("openai-proxy: vault cleanup of %s failed: %v", anonID, err)
			}
		}()
	}

	outBody, err := json.Marshal(req)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "anonde_error",
			"re-encode request: "+err.Error())
		return
	}

	upstream, err := p.forward(ctx, outBody)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "anonde_error",
			"upstream request failed: "+err.Error())
		return
	}
	defer upstream.Body.Close()

	respBody, err := io.ReadAll(upstream.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "anonde_error",
			"read upstream response: "+err.Error())
		return
	}

	// Pass an upstream error (non-2xx) straight through — it carries no
	// revealable content and the client expects the upstream's own
	// error shape. Same for the (rare) request with nothing to reveal.
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 || anonID == "" {
		writeUpstream(w, upstream, respBody)
		return
	}

	revealed, err := p.revealResponse(ctx, tenant, anonID, respBody)
	if err != nil {
		log.Printf("openai-proxy: reveal failed: %v", err)
		writeOpenAIError(w, http.StatusBadGateway, "anonde_error",
			"de-anonymize response: "+err.Error())
		return
	}
	writeUpstream(w, upstream, revealed)
}

// anonymizeSegments anonymizes every segment in one Service.Ingest call
// by packing them into a JSON object keyed by index. Doing it in one
// call (rather than one per segment) means the same cleartext gets the
// same token across messages — the upstream model sees a consistent
// conversation. Returns the anonymized segments in input order plus the
// minted anonymization id used later to reveal the response.
func (p *openAIProxy) anonymizeSegments(ctx context.Context, tenant string, segs []string) ([]string, string, error) {
	obj := make(map[string]string, len(segs))
	for i, s := range segs {
		obj[strconv.Itoa(i)] = s
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, "", err
	}
	resp, err := p.svc.Ingest(ctx, core.IngestRequest{
		TenantID:      tenant,
		Content:       string(raw),
		ContentFormat: "json",
	})
	if err != nil {
		return nil, "", err
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(resp.AnonymizedContent), &out); err != nil {
		return nil, "", fmt.Errorf("parse anonymized content: %w", err)
	}
	result := make([]string, len(segs))
	for i := range segs {
		result[i] = out[strconv.Itoa(i)]
	}
	return result, resp.ID, nil
}

// revealResponse de-anonymizes choices[].message.content in an OpenAI
// chat-completion response. A body we can't parse as the expected shape
// is returned unchanged rather than treated as an error — the worst
// case is the client sees tokens, never raw PII, and never a 500 on a
// response shape we simply didn't anticipate.
func (p *openAIProxy) revealResponse(ctx context.Context, tenant, id string, body []byte) ([]byte, error) {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return body, nil
	}
	rawChoices, ok := resp["choices"]
	if !ok {
		return body, nil
	}
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(rawChoices, &choices); err != nil {
		return body, nil
	}

	changed := false
	for _, choice := range choices {
		msgRaw, ok := choice["message"]
		if !ok {
			continue
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(msgRaw, &msg) != nil {
			continue
		}
		var content string
		if json.Unmarshal(msg["content"], &content) != nil || content == "" {
			// content is absent, empty, or non-string (e.g. a
			// tool-call-only message) — nothing to reveal.
			continue
		}
		rev, err := p.svc.Reveal(ctx, core.RevealRequest{
			TenantID:      tenant,
			ID:            id,
			Actor:         proxyActor,
			Purpose:       proxyPurpose,
			Content:       content,
			ContentFormat: "text",
		})
		if err != nil {
			return nil, err
		}
		revContent, _ := json.Marshal(rev.DeanonymizedContent)
		msg["content"] = revContent
		newMsg, _ := json.Marshal(msg)
		choice["message"] = newMsg
		changed = true
	}
	if !changed {
		return body, nil
	}
	newChoices, _ := json.Marshal(choices)
	resp["choices"] = newChoices
	return json.Marshal(resp)
}

// forward POSTs the (anonymized) request body to the upstream provider.
func (p *openAIProxy) forward(ctx context.Context, body []byte) (*http.Response, error) {
	url := p.cfg.UpstreamBaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.UpstreamAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.UpstreamAPIKey)
	}
	return p.client.Do(httpReq)
}

// segJob is one anonymizable text segment plus the closure that writes
// the anonymized text back into the parsed request structure.
type segJob struct {
	text string
	set  func(anonymized string)
}

// collectSegmentJobs returns one job per anonymizable text segment in a
// chat message. A string `content` yields a single job. An array
// `content` (the multimodal shape) yields one job per `{"type":"text"}`
// part and leaves image / audio / file parts untouched.
func collectSegmentJobs(msg map[string]json.RawMessage) []*segJob {
	contentRaw, ok := msg["content"]
	if !ok || len(contentRaw) == 0 {
		return nil
	}

	var asString string
	if json.Unmarshal(contentRaw, &asString) == nil {
		return []*segJob{{
			text: asString,
			set: func(v string) {
				b, _ := json.Marshal(v)
				msg["content"] = b
			},
		}}
	}

	var parts []map[string]json.RawMessage
	if json.Unmarshal(contentRaw, &parts) != nil {
		return nil
	}
	jobs := make([]*segJob, 0, len(parts))
	for _, part := range parts {
		var typ string
		_ = json.Unmarshal(part["type"], &typ)
		if typ != "text" {
			continue
		}
		var text string
		if json.Unmarshal(part["text"], &text) != nil {
			continue
		}
		part := part // capture per iteration
		jobs = append(jobs, &segJob{
			text: text,
			set: func(v string) {
				b, _ := json.Marshal(v)
				part["text"] = b
				// Re-marshal the whole parts array — the maps are
				// mutated in place, so this picks up every part.
				full, _ := json.Marshal(parts)
				msg["content"] = full
			},
		})
	}
	return jobs
}

// parseModelProvider splits an OpenRouter-style "provider/model" model
// id. A bare model (no slash) or an absent / non-string field yields
// the default provider with hadPrefix=false and the model unchanged.
// When a prefix is present the provider segment is lower-cased and
// trimmed, and model is the remainder after the first slash.
func parseModelProvider(raw json.RawMessage) (provider, model string, hadPrefix bool) {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return defaultProvider, "", false
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return strings.ToLower(strings.TrimSpace(s[:i])), s[i+1:], true
	}
	return defaultProvider, s, false
}

// rawBool reports whether a json.RawMessage decodes to JSON true.
// Absent / non-bool values are treated as false.
func rawBool(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

// writeUpstream relays an upstream response (status + body) to the
// client, preserving the upstream Content-Type.
func writeUpstream(w http.ResponseWriter, upstream *http.Response, body []byte) {
	ct := upstream.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(upstream.StatusCode)
	_, _ = w.Write(body)
}

// writeOpenAIError writes an OpenAI-shaped error object so SDK clients
// surface it through their normal error path.
func writeOpenAIError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errType,
		},
	})
}
