//go:build stress

package stress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// cluster_test.go is the multi-backend topology test. Spins up N
// containers behind the sticky-session proxy in cluster.go and
// verifies:
//
//   - Sticky routing — repeated requests for the same (tenant, id)
//     land on the same backend. Without sticky routing the vault
//     lookup misses on reveal.
//   - End-to-end round-trip across the cluster — anonymize → reveal
//     still returns the original cleartext byte-exactly.
//   - Distribution — over enough (tenant, id) keys, every backend
//     gets work. A degenerate proxy that always picks backend[0]
//     would still pass the round-trip; the distribution assertion
//     is what proves sticky-hash is actually doing work.
//
// Runs against the patterns variant by default — the test exercises
// the proxy + topology, not the analyzer. NER variants would work
// identically but each container drag is +500-1400 MB.

const (
	clusterSize   = 3
	clusterTenant = "stress-cluster"
)

func TestStress_Cluster_StatefulRoundTrip(t *testing.T) {
	ctx := context.Background()
	// Patterns-only: this test exercises the proxy + topology, not
	// the NER pipeline. Switch to NERVariants() if you specifically
	// want to stress the cluster path with GLiNER inference cost.
	v := Variants[0]
	if v.Name != "patterns" {
		t.Fatalf("expected Variants[0] = patterns, got %q (stress matrix reordered?)", v.Name)
	}

	c := StartCluster(ctx, t, v, clusterSize)
	t.Cleanup(func() { c.Stop(ctx) })

	httpc := &http.Client{Timeout: 20 * time.Second}

	const numDocs = 30
	docs := make([]clusterDoc, numDocs)

	// Step 1: anonymize numDocs documents through the proxy. Server
	// mints ids; the proxy intercepts and rewrites the body with a
	// proxy-minted id so it can hash (tenant, id) → backend.
	for i := 0; i < numDocs; i++ {
		original := fmt.Sprintf("Doc %d for cluster test. Email leak-%d@example.com.", i, i)
		body, _ := json.Marshal(map[string]any{
			"tenant_id":      clusterTenant,
			"content_format": "text",
			"content":        original,
		})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.ProxyURL+"/v1/anonymizations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpc.Do(req)
		if err != nil {
			t.Fatalf("anonymize %d: %v", i, err)
		}
		var out map[string]any
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("anonymize %d status=%d body=%s", i, resp.StatusCode, raw)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("anonymize %d decode: %v", i, err)
		}
		id, _ := out["id"].(string)
		anon, _ := out["anonymized_content"].(string)
		if id == "" || anon == "" {
			t.Fatalf("anonymize %d missing fields: %v", i, out)
		}
		// The proxy rewrites the body to inject its own id. The
		// response should echo that id back.
		if !strings.HasPrefix(id, "anon_") {
			t.Fatalf("anonymize %d: id=%q, want anon_<hex>", i, id)
		}
		docs[i] = clusterDoc{
			ID:       id,
			Original: original,
			Anon:     anon,
		}
	}

	// Step 2: distribution check. Every backend should have served
	// at least one anonymize. A degenerate proxy that always picked
	// backend[0] would fail here.
	hits := c.BackendStats()
	t.Logf("cluster: per-backend hit counts after anonymize: %v", hits)
	for i, h := range hits {
		if h == 0 {
			t.Errorf("cluster: backend[%d] received zero anonymize requests — sticky-hash distribution is broken", i)
		}
	}

	// Step 3: reveal every doc. Sticky routing should land each
	// reveal on the same backend that minted the vault entry; the
	// reveal must recover the original cleartext byte-exactly.
	for i, d := range docs {
		body, _ := json.Marshal(map[string]any{
			"tenant_id":      clusterTenant,
			"actor":          "stress",
			"purpose":        "cluster",
			"content_format": "text",
			"content":        d.Anon,
		})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.ProxyURL+"/v1/anonymizations/"+d.ID+"/reveal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpc.Do(req)
		if err != nil {
			t.Fatalf("reveal %d: %v", i, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("reveal %d status=%d body=%s (sticky routing broken? id=%s)", i, resp.StatusCode, raw, d.ID)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("reveal %d decode: %v", i, err)
		}
		got, _ := out["deanonymized_content"].(string)
		if got != d.Original {
			t.Fatalf("reveal %d did not recover original byte-exactly:\n got: %q\nwant: %q", i, got, d.Original)
		}
	}

	// Step 4: post-test distribution sanity. Reveals should mirror
	// the anonymize distribution (sticky-hash is deterministic), so
	// the per-backend hit counts should have grown ~2× across the
	// board (anonymize + reveal per doc). We don't assert exact
	// counts — just that every backend grew.
	postHits := c.BackendStats()
	t.Logf("cluster: per-backend hit counts after reveal: %v", postHits)
	for i := range postHits {
		if postHits[i] < 2*hits[i] {
			t.Errorf("cluster: backend[%d] hits grew %d → %d, expected ~2x (reveal not routed to mint backend?)",
				i, hits[i], postHits[i])
		}
	}
}

type clusterDoc struct {
	ID       string
	Original string
	Anon     string
}
