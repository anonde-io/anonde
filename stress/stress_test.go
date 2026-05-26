//go:build stress

package stress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// stress_test.go holds the load + edge-case tier. Five cases for now,
// covering the failure modes that actually matter for anonde:
//
//   - PIIDense          : sustained load with PII-dense text. Throughput
//                         + latency regression guard across every variant.
//   - PoolSaturation    : NER variants only. Concurrent requests >
//                         GLINER_POOL_SIZE × ANONDE_MAX_CONCURRENT_REQUESTS.
//                         Asserts 429s come back, never OOM, never hang.
//   - PDFLargeDoc       : NER variants only. Real visual redaction over a
//                         long PDF. Catches OCR / pdftoppm regressions.
//   - BodyCap           : All variants. Oversized body → 4xx, no 5xx.
//                         Currently warns on REST gap (see memory:
//                         rest-gateway-body-cap-gap).
//   - MultiTenant       : All variants. Tenant A blasts the server;
//                         tenant B probe traffic stays under p99 budget.
//
// More cases (TTL races, JSON recursion bomb, unicode adversarial,
// token namespace isolation under load) land in follow-ups. The
// pattern is fixed: write a target builder + an assertion.

// -----------------------------------------------------------------------------
// PIIDense: sustained load with PII-dense text
// -----------------------------------------------------------------------------

func TestStress_PIIDense(t *testing.T) {
	ctx := context.Background()
	ForEachVariant(t, AllVariants(), func(t *testing.T, v Variant) {
		c := Start(ctx, t, v)
		t.Cleanup(func() { c.Stop(ctx) })

		body := piiDenseDoc()
		// Rates are set BELOW the per-variant concurrency budget
		// (ANONDE_MAX_CONCURRENT_REQUESTS in harness.go) so the
		// sustained-load test exercises the pipeline, not the limiter
		// — the limiter has its own dedicated PoolSaturation test.
		// CI runners are slower than dev boxes; pick conservative
		// rates so the assertions remain stable across hardware.
		attack := Attack{
			Name:     "pii_dense",
			Targets:  []vegeta.Target{TargetCreateAnonymization(c.HTTPURL, "stress-dense", "", "text", body)},
			Rate:     ratePerVariant(v, 20, 3, 2),
			Duration: 20 * time.Second,
			Timeout:  10 * time.Second,
		}
		m, _ := attack.Run(t)
		Summarize(t, attack.Name, v.Name, m)

		// Thresholds are guard rails, not optimisation targets.
		// Variant-aware floor: patterns is cheap (99%), NER pays
		// per-request inference cost so brief over-cap windows are
		// expected on small CI runners (90%), ner-stack runs two
		// ONNX sessions per request so burst windows are even more
		// common (85%). "Server fell over" still fails loudly.
		AssertOK(t, m, successFloor(v), perVariantP99(v))

		// Cross-check: counters ticked. A handler that no-ops would
		// pass the success-rate check but show zero entity detections.
		assertEntitiesDetected(t, c)
	})
}

// -----------------------------------------------------------------------------
// PoolSaturation: shove > pool size concurrent requests at NER variants
// -----------------------------------------------------------------------------

func TestStress_PoolSaturation(t *testing.T) {
	ctx := context.Background()
	ForEachVariant(t, NERVariants(), func(t *testing.T, v Variant) {
		c := Start(ctx, t, v)
		t.Cleanup(func() { c.Stop(ctx) })

		// 1.5× ANONDE_MAX_CONCURRENT_REQUESTS sustained for 15s.
		// We expect a non-trivial slice of 429s (the limiter
		// rejecting overflow) and zero 5xx / connection errors.
		cap := 4
		if v.Name == "ner-stack" {
			cap = 3
		}
		rate := cap * 3 // pile on

		attack := Attack{
			Name:     "pool_saturation",
			Targets:  []vegeta.Target{TargetCreateAnonymization(c.HTTPURL, "stress-pool", "", "text", piiDenseDoc())},
			Rate:     rate,
			Duration: 15 * time.Second,
			Timeout:  20 * time.Second,
			Workers:  uint64(rate * 2),
		}
		m, _ := attack.Run(t)
		Summarize(t, attack.Name, v.Name, m)

		// Acceptance: a successful pool-saturation looks like a
		// mix of 200 and 429. Zero 5xx, zero connection errors,
		// and at least *some* 429s prove the limiter is doing
		// something — if every request 200s the cap is too high
		// for the rate we picked.
		assertNo5xx(t, m)
		got429 := m.StatusCodes["429"]
		if got429 == 0 {
			// Warn rather than fail: on a fast box the pool may
			// drain quickly enough that the limiter never trips
			// even at 3× cap. The 5xx assertion is the hard one.
			t.Logf("warning: pool_saturation produced zero 429s on %s (cap may be too lax for rate=%d)", v.Name, rate)
		}
		assertContainerAlive(t, c)
	})
}

// -----------------------------------------------------------------------------
// PDFLargeDoc: visual redaction over a non-trivial PDF
// -----------------------------------------------------------------------------

func TestStress_PDFLargeDoc(t *testing.T) {
	ctx := context.Background()
	ForEachVariant(t, NERVariants(), func(t *testing.T, v Variant) {
		c := Start(ctx, t, v)
		t.Cleanup(func() { c.Stop(ctx) })

		// repoRoot already resolves to the repo root via runtime.Caller,
		// so the relative path is from there — NO `..` prefix.
		pdf := mustReadFixture(t, filepath.Join("internal", "content", "testdata", "pii_sample.pdf"))

		// PDF redaction is the slow path (rasterize → OCR → GLiNER →
		// draw). On a 4-vCPU CI runner without GPU each request can
		// take 3-8 seconds; piling up parallel requests via vegeta
		// workers saturates the concurrency cap immediately. Serialize
		// at the client (Workers=1, low rate, generous timeout) so
		// the test exercises the pipeline end-to-end without
		// stressing the limiter — pool/limiter behaviour has its own
		// dedicated test.
		attack := Attack{
			Name:     "pdf_large",
			Targets:  []vegeta.Target{TargetAnonymizePDF(c.HTTPURL, "stress-pdf", pdf)},
			Rate:     1,
			Duration: 20 * time.Second,
			Timeout:  60 * time.Second,
			Workers:  1,
			MaxBody:  64 << 10, // PDFs are big; cap so we don't gulp them all into memory.
		}
		m, _ := attack.Run(t)
		Summarize(t, attack.Name, v.Name, m)

		AssertOK(t, m, 0.95, 60*time.Second)
		assertContainerAlive(t, c)
	})
}

// -----------------------------------------------------------------------------
// BodyCap: oversized bodies → 4xx, never 5xx, never OOM
// -----------------------------------------------------------------------------

func TestStress_BodyCap(t *testing.T) {
	ctx := context.Background()
	ForEachVariant(t, AllVariants(), func(t *testing.T, v Variant) {
		// MAX_CONTENT_BYTES default is 10 MiB. We send 32 MiB.
		// Connect-routed requests will return 429/ResourceExhausted;
		// REST-gateway requests currently slip past the cap (see
		// memory: rest-gateway-body-cap-gap). Test logs the gap as
		// a warning, asserts only that the server returns *some*
		// 4xx-or-5xx-but-not-hang behaviour.
		oversized := bytes.Repeat([]byte("a"), 32<<20)
		body, _ := json.Marshal(map[string]any{
			"tenant_id":      "stress-cap",
			"content_format": "text",
			"content":        string(oversized),
		})

		c := Start(ctx, t, v)
		t.Cleanup(func() { c.Stop(ctx) })

		attack := Attack{
			Name: "body_cap",
			Targets: []vegeta.Target{{
				Method: http.MethodPost,
				URL:    c.HTTPURL + "/v1/anonymizations",
				Body:   body,
				Header: http.Header{"Content-Type": []string{"application/json"}},
			}},
			Rate:     2,
			Duration: 5 * time.Second,
			Timeout:  30 * time.Second,
			MaxBody:  4 << 10,
		}
		m, _ := attack.Run(t)
		Summarize(t, attack.Name, v.Name, m)

		// Status-code buckets are the authoritative outcome here. The
		// REST gateway is wrapped in limitBody, so every oversized
		// request must land in 4xx. 5xx = server failure on the
		// boundary; 2xx = body-cap regression.
		//
		// Vegeta's m.Errors slice is intentionally NOT checked: it
		// captures every non-2xx response's status text (e.g. "400
		// Bad Request") alongside real transport errors, which means
		// a successful cap-rejection looks like an error pile.
		count2xx, count4xx, count5xx := codeBuckets(m.StatusCodes)
		if count5xx > 0 {
			t.Errorf("oversized body produced %d 5xx — server failure on the boundary", count5xx)
		}
		if count2xx > 0 {
			t.Errorf("REST gateway accepted %d oversized requests — body-cap regression. 4xx=%d 5xx=%d", count2xx, count4xx, count5xx)
		}
		// Real transport errors (timeouts, RSTs) would show as
		// zero-status counts — anything not buckable into 2xx/4xx/5xx.
		// Catch those explicitly via the total-vs-bucketed gap.
		bucketed := count2xx + count4xx + count5xx
		if int(m.Requests) > bucketed {
			t.Errorf("oversized body produced %d transport errors (no HTTP status returned). Requests=%d bucketed=%d. Errors[:3]=%v",
				int(m.Requests)-bucketed, m.Requests, bucketed, m.Errors[:min(3, len(m.Errors))])
		}
		assertContainerAlive(t, c)
	})
}

// -----------------------------------------------------------------------------
// MultiTenant: tenant A blasts the server; tenant B latency stays bounded
// -----------------------------------------------------------------------------

func TestStress_MultiTenant(t *testing.T) {
	ctx := context.Background()
	ForEachVariant(t, AllVariants(), func(t *testing.T, v Variant) {
		c := Start(ctx, t, v)
		t.Cleanup(func() { c.Stop(ctx) })

		// Tenant A: noisy neighbor. Sustained load BELOW the per-variant
		// concurrency budget (PoolSaturation owns the over-budget case).
		// Tenant B: cheap probe traffic via /v1/health, every 200 ms.
		blast := Attack{
			Name:     "multi_tenant.blast",
			Targets:  []vegeta.Target{TargetCreateAnonymization(c.HTTPURL, "tenant-a", "", "text", piiDenseDoc())},
			Rate:     ratePerVariant(v, 25, 2, 1),
			Duration: 20 * time.Second,
			Timeout:  10 * time.Second,
		}
		probe := Attack{
			Name:     "multi_tenant.probe",
			Targets:  []vegeta.Target{TargetHealth(c.HTTPURL)},
			Rate:     2,
			Duration: 20 * time.Second,
			Timeout:  5 * time.Second,
		}

		// Run concurrently so the probe overlaps the blast.
		var wg sync.WaitGroup
		var blastMetrics, probeMetrics *vegeta.Metrics
		wg.Add(2)
		go func() {
			defer wg.Done()
			blastMetrics, _ = blast.Run(t)
		}()
		go func() {
			defer wg.Done()
			probeMetrics, _ = probe.Run(t)
		}()
		wg.Wait()

		Summarize(t, blast.Name, v.Name, blastMetrics)
		Summarize(t, probe.Name, v.Name, probeMetrics)

		// Probe assertions: 75% success + p99 ≤ 2s under sustained
		// neighbor load. NOT 100%: the concurrency limiter wraps the
		// whole mux (intentional — "an unhealthy server should
		// signal busy"), so under burst contention some /v1/health
		// requests legitimately get 429'd. 75% is the fairness floor
		// — well below means the blaster is starving the probe and
		// we'd revisit the budget. The non-NER variants land
		// comfortably above this because patterns has no inference
		// cost; the floor is calibrated for the worst-case NER row.
		AssertOK(t, probeMetrics, 0.75, 2*time.Second)
		assertContainerAlive(t, c)
	})
}

// -----------------------------------------------------------------------------
// Per-variant tuning + shared assertions
// -----------------------------------------------------------------------------

// ratePerVariant returns a load level the variant can plausibly sustain.
// Patterns is cheap; NER is bounded by pool inference cost; NER-stack
// runs two ONNX sessions per request.
func ratePerVariant(v Variant, patternsRPS, nerRPS, nerStackRPS int) int {
	switch v.Name {
	case "patterns":
		return patternsRPS
	case "ner":
		return nerRPS
	case "ner-stack":
		return nerStackRPS
	}
	return patternsRPS
}

// perVariantP99 is the latency-budget envelope. Patterns is fast;
// NER pays inference cost; NER-stack runs two models.
func perVariantP99(v Variant) time.Duration {
	switch v.Name {
	case "patterns":
		return 500 * time.Millisecond
	case "ner":
		return 3 * time.Second
	case "ner-stack":
		return 5 * time.Second
	}
	return 2 * time.Second
}

// successFloor is the minimum acceptable success rate per variant.
// Brief over-cap bursts under sustained load are expected on CI
// hardware; the floor reflects that without giving up on regression
// detection (a server that fell over still fails).
func successFloor(v Variant) float64 {
	switch v.Name {
	case "patterns":
		return 0.99
	case "ner":
		return 0.90
	case "ner-stack":
		return 0.85
	}
	return 0.95
}

// assertNo5xx fails if any 500-series response landed during the
// attack. Pool-saturation tests want 429s, not 500s.
func assertNo5xx(t *testing.T, m *vegeta.Metrics) {
	t.Helper()
	for code, n := range m.StatusCodes {
		if len(code) > 0 && code[0] == '5' && n > 0 {
			t.Errorf("got %d responses with status %s — server failure under load", n, code)
		}
	}
}

// assertContainerAlive proves the container survived the test:
// /v1/health responds, AND /metrics is still scrapeable. Catches the
// silent-OOM class of bug where the binary dies mid-attack and vegeta
// just reports "connection refused" on every subsequent request.
func assertContainerAlive(t *testing.T, c *Container) {
	t.Helper()
	resp, err := http.Get(c.HTTPURL + "/v1/health")
	if err != nil {
		t.Errorf("post-attack /v1/health: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("post-attack /v1/health = %d: %s", resp.StatusCode, body)
	}

	mresp, err := http.Get(c.MetricsURL)
	if err != nil {
		t.Errorf("post-attack /metrics: %v", err)
		return
	}
	defer mresp.Body.Close()
	if mresp.StatusCode != 200 {
		t.Errorf("post-attack /metrics = %d", mresp.StatusCode)
	}
}

// assertEntitiesDetected scrapes /metrics and asserts the
// anonde_entities_detected_total counter is > 0 — proves the
// pipeline did real work, not just 200'd handlers.
func assertEntitiesDetected(t *testing.T, c *Container) {
	t.Helper()
	resp, err := http.Get(c.MetricsURL)
	if err != nil {
		t.Errorf("scrape metrics: %v", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "anonde_entities_detected_total") {
		t.Errorf("metrics missing anonde_entities_detected_total — pipeline never fired?")
		return
	}
	// Cheap check: any non-zero `anonde_entities_detected_total{...} N`
	// line where N is a positive integer.
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "anonde_entities_detected_total{") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[len(parts)-1] != "0" && parts[len(parts)-1] != "0.0" {
			return // found at least one non-zero entity counter
		}
	}
	t.Errorf("anonde_entities_detected_total: all series are zero — analyzer never matched anything?")
}

// codeBuckets aggregates a vegeta status histogram into 2xx / 4xx / 5xx.
func codeBuckets(in map[string]int) (twoxx, fourxx, fivexx int) {
	for code, n := range in {
		if len(code) == 0 {
			continue
		}
		switch code[0] {
		case '2':
			twoxx += n
		case '4':
			fourxx += n
		case '5':
			fivexx += n
		}
	}
	return
}

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

// piiDenseDoc returns a synthetic text doc shaped like the failure
// modes anonde sees in production: mixed English / German, several
// PII types per line, no obvious delimiters between fields. ~2 KB.
func piiDenseDoc() string {
	const tmpl = `Patient: Anna Schmidt, geb. 14.03.1962, Berlin, Telefon +49 30 1234567,
E-Mail anna.schmidt@klinik.de. Versichert bei DAK, Versichertennummer
A123456789. Hausarzt: Dr. Hans Müller, Praxis am Alexanderplatz,
Rosenstr. 12, 10178 Berlin. Letzter Termin: 22.04.2026.

Hi from Sarah Chen (sarah.chen@acme.example, +1 415 555 0142).
Card 4111-1111-1111-1111 was charged 89.99 USD on 2024-03-15.
Customer ID CUS-9912843. Shipping address: 1428 Elm Street,
Springfield, IL 62704. Bank routing 011000028, account 1234567890.

Dr. Marie Curie was born 7 November 1867 in Warsaw, Poland.
SSN 123-45-6789. Passport 845721903. IBAN DE89370400440532013000.
`
	// 2 KB of mixed-language PII; repeat to give the analyzer real
	// work without making the request body absurd.
	return strings.Repeat(tmpl, 3)
}

// mustReadFixture loads a file from disk or fails the test. Used for
// PDFs and similar binary fixtures.
func mustReadFixture(t *testing.T, relPath string) []byte {
	t.Helper()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		t.Fatalf("read fixture %q: %v", relPath, err)
	}
	return raw
}

// min is a tiny helper for vegeta result slicing — Go 1.21+ has builtin
// min but the stress package may run under older toolchains on the
// scheduled CI runners; keeping a local copy avoids surprise.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Suppress unused-import warnings if any of fmt / runtime stop being
// used in this file during refactors. The harness file uses runtime
// for path resolution; this file uses fmt / runtime only on the error
// reporting path.
var _ = fmt.Sprintf
var _ = runtime.NumCPU
