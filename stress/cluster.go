//go:build stress

package stress

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// cluster.go runs N anonde containers behind a sticky-session
// reverse proxy. Why sticky and not round-robin: anonde's vault +
// store are per-process in-memory — a token minted by container 1
// can't be revealed by container 2. The proxy hashes (tenant_id, id)
// and pins every request for that key to one backend, so a doc's
// full lifecycle (anonymize → reveal → delete) lands on the same
// node. This mirrors the real horizontal-scale deployment pattern
// you'd use until anonde grows a shared store backend.
//
// What this DOES test:
//   - Request distribution across N backends (load is spread).
//   - Stateful round-trips survive multi-backend topology when the
//     proxy routes consistently.
//   - The pipeline still functions end-to-end through one extra hop.
//
// What this does NOT test (deferred until anonde has shared state):
//   - Backend-down failover. A backend dying kills every key mapped
//     to it; consistent hashing would help but doesn't change the
//     fact that the doc's vault entry vanished with the process.
//   - Shared-state read-after-write across backends.

// Cluster is N backends + an in-process sticky proxy. Created via
// StartCluster, torn down via Stop (also via t.Cleanup if the caller
// wires it that way).
type Cluster struct {
	Variant    Variant
	Containers []*Container
	ProxyURL   string // Client-facing URL. Hit this, not the backends directly.

	proxyServer   *http.Server
	proxyListener net.Listener
}

// StartCluster spins up n backends for the given variant, then starts
// an in-process sticky-session proxy in front of them. Returns when
// every backend reports healthy AND the proxy is listening.
//
// Backends are started serially to keep Docker layer cache hits
// deterministic. Parallel start would shave seconds but add log noise
// when one variant pulls 770 MB of model weights while another
// concurrently rebuilds the same image.
func StartCluster(ctx context.Context, t *testing.T, v Variant, n int) *Cluster {
	t.Helper()
	if n < 2 {
		t.Fatalf("StartCluster: n=%d, need at least 2 backends (use Start for single)", n)
	}

	containers := make([]*Container, n)
	for i := 0; i < n; i++ {
		containers[i] = Start(ctx, t, v)
	}

	backends := make([]string, n)
	for i, c := range containers {
		backends[i] = c.HTTPURL
	}

	proxy := newStickyProxy(backends)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		for _, c := range containers {
			c.Stop(ctx)
		}
		t.Fatalf("cluster: listen: %v", err)
	}
	server := &http.Server{
		Handler:      proxy,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()

	return &Cluster{
		Variant:       v,
		Containers:    containers,
		ProxyURL:      "http://" + listener.Addr().String(),
		proxyServer:   server,
		proxyListener: listener,
	}
}

// Stop tears down the proxy (best-effort) then every backend. Safe to
// call multiple times.
func (c *Cluster) Stop(ctx context.Context) {
	if c == nil {
		return
	}
	if c.proxyServer != nil {
		shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = c.proxyServer.Shutdown(shutCtx)
		cancel()
		c.proxyServer = nil
	}
	for _, container := range c.Containers {
		container.Stop(ctx)
	}
	c.Containers = nil
}

// BackendStats returns the per-backend hit count the proxy has seen
// since boot. Test code uses this to assert load distribution.
func (c *Cluster) BackendStats() []int {
	p, ok := c.proxyServer.Handler.(*stickyProxy)
	if !ok {
		return nil
	}
	return p.snapshotHits()
}

// -----------------------------------------------------------------------------
// Sticky proxy
// -----------------------------------------------------------------------------

// stickyProxy hashes (tenant_id, anon_id) → backend index and forwards.
// When the inbound request creates a new anonymization without
// supplying an id, the proxy mints one and rewrites the JSON body so
// the backend stores it under the same key the proxy will hash on
// future requests for the same doc.
type stickyProxy struct {
	backends []*httputil.ReverseProxy
	urls     []string
	hits     []int
	hitsMu   sync.Mutex
}

func newStickyProxy(backendURLs []string) *stickyProxy {
	ps := make([]*httputil.ReverseProxy, len(backendURLs))
	for i, raw := range backendURLs {
		u, err := url.Parse(raw)
		if err != nil {
			panic("cluster: bad backend URL " + raw + ": " + err.Error())
		}
		ps[i] = httputil.NewSingleHostReverseProxy(u)
	}
	return &stickyProxy{
		backends: ps,
		urls:     backendURLs,
		hits:     make([]int, len(backendURLs)),
	}
}

func (p *stickyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	idx, err := p.route(r)
	if err != nil {
		http.Error(w, "cluster proxy: "+err.Error(), http.StatusBadRequest)
		return
	}
	p.recordHit(idx)
	p.backends[idx].ServeHTTP(w, r)
}

func (p *stickyProxy) snapshotHits() []int {
	p.hitsMu.Lock()
	defer p.hitsMu.Unlock()
	out := make([]int, len(p.hits))
	copy(out, p.hits)
	return out
}

func (p *stickyProxy) recordHit(idx int) {
	p.hitsMu.Lock()
	p.hits[idx]++
	p.hitsMu.Unlock()
}

// route picks the backend index for r. It also mutates r in-place when
// the request creates an anonymization without an id: the proxy mints
// one and rewrites the JSON body so subsequent reveal calls hash to
// the same backend.
func (p *stickyProxy) route(r *http.Request) (int, error) {
	tenantID, anonID, err := p.extractKey(r)
	if err != nil {
		return 0, err
	}
	if tenantID == "" {
		// Stateless route (health, version, /metrics). Round-robin by
		// hit count keeps the distribution roughly even without
		// keeping a separate counter.
		return p.fewestHits(), nil
	}
	if anonID == "" {
		// POST /v1/anonymizations without a caller id. Mint one,
		// rewrite the body, then hash.
		anonID = "anon_" + randomHex(8)
		if err := p.rewriteBodyWithID(r, anonID); err != nil {
			return 0, fmt.Errorf("rewrite body: %w", err)
		}
	}
	key := tenantID + "|" + anonID
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32()) % len(p.backends), nil
}

// extractKey pulls (tenant_id, id) out of r based on the URL pattern.
// Returns ("", "", nil) for routes the proxy treats as stateless.
func (p *stickyProxy) extractKey(r *http.Request) (string, string, error) {
	path := r.URL.Path
	switch {
	case path == "/v1/anonymizations" && r.Method == http.MethodPost:
		// Body-bound POST. Read tenant_id; id may be empty (server-mint).
		body, err := bufferBody(r)
		if err != nil {
			return "", "", err
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			return "", "", fmt.Errorf("decode anonymize body: %w", err)
		}
		t, _ := doc["tenant_id"].(string)
		if t == "" {
			t, _ = doc["tenantId"].(string)
		}
		id, _ := doc["id"].(string)
		return t, id, nil

	case path == "/v1/anonymizations/pdf" && r.Method == http.MethodPost:
		// Raw bytes; tenant in header or query. id is server-minted
		// for this surface; we hash on tenant alone, which means a
		// busy tenant pins to one backend — acceptable for the
		// stress-test corpus, document the limitation.
		t := pdfTenant(r)
		return t, "", nil

	case strings.HasPrefix(path, "/v1/anonymizations/") && strings.HasSuffix(path, "/reveal-pdf") && r.Method == http.MethodGet:
		// GET reveal-pdf — id in path, tenant in header/query.
		id := extractIDFromPath(path, "reveal-pdf")
		t := pdfTenant(r)
		return t, id, nil

	case strings.HasPrefix(path, "/v1/anonymizations/") && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
		// /v1/anonymizations/{id}/{verb} (POST) or
		// DELETE /v1/anonymizations/{id}.
		id := extractIDFromPath(path, "")
		t, err := tenantFromRequest(r)
		if err != nil {
			return "", "", err
		}
		return t, id, nil
	}
	// Stateless: /v1/health, /v1/version, /v1/synthesize, /v1/chat/completions, /metrics, etc.
	return "", "", nil
}

// rewriteBodyWithID re-buffers the request body with the proxy-minted
// id inserted into the JSON object. Used when the caller POSTs to
// /v1/anonymizations without one — the backend would mint its own
// id and tie the vault entry to that, which the proxy wouldn't be
// able to route reveals to without a separate lookup table.
func (p *stickyProxy) rewriteBodyWithID(r *http.Request, id string) error {
	body, err := bufferBody(r)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode anonymize body: %w", err)
	}
	doc["id"] = id
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode rewritten body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r.Header.Set("Content-Length", fmt.Sprintf("%d", len(raw)))
	return nil
}

// bufferBody reads and replaces r.Body so it can be read again
// downstream. Required for any path where the proxy needs to peek
// at the body before forwarding.
func bufferBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// fewestHits returns the backend index that has served the fewest
// requests so far. Used for stateless routes; keeps distribution
// even without bolting on a separate round-robin counter.
func (p *stickyProxy) fewestHits() int {
	p.hitsMu.Lock()
	defer p.hitsMu.Unlock()
	min := p.hits[0]
	idx := 0
	for i, h := range p.hits {
		if h < min {
			min = h
			idx = i
		}
	}
	return idx
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func extractIDFromPath(path, trailingVerb string) string {
	// /v1/anonymizations/<id>           → <id>
	// /v1/anonymizations/<id>/reveal    → <id>
	// /v1/anonymizations/<id>/reveal-pdf → <id>
	const prefix = "/v1/anonymizations/"
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	// /v1/anonymizations/<id> (DELETE)
	if q := strings.Index(rest, "?"); q >= 0 {
		return rest[:q]
	}
	return rest
}

func pdfTenant(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Anonde-Tenant")); v != "" {
		return v
	}
	q := r.URL.Query()
	for _, k := range []string{"tenant", "tenant_id", "tenantId"} {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func tenantFromRequest(r *http.Request) (string, error) {
	q := r.URL.Query()
	for _, k := range []string{"tenant", "tenant_id", "tenantId"} {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v, nil
		}
	}
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodDelete {
		// No body to peek at — header is the last resort.
		if v := strings.TrimSpace(r.Header.Get("X-Anonde-Tenant")); v != "" {
			return v, nil
		}
		return "", nil
	}
	body, err := bufferBody(r)
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", nil
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("decode body for tenant: %w", err)
	}
	if t, _ := doc["tenant_id"].(string); t != "" {
		return t, nil
	}
	if t, _ := doc["tenantId"].(string); t != "" {
		return t, nil
	}
	return "", nil
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
