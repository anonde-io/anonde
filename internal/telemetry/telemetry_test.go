package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anonde-io/anonde/internal/metrics"
)

// TestHeartbeatFieldAllowlist is the load-bearing privacy gate. The
// telemetry section of the launch plan calls this out explicitly:
//
//	"serialize the heartbeat payload in a unit test and assert
//	against a hard-coded allowlist of fields. Any new field requires
//	a deliberate code change (no accidental leakage of new state)."
//
// The contract is two-sided:
//
//   - Adding a Heartbeat struct field without updating
//     HeartbeatAllowedFields fails here (new key not in allowlist).
//   - Removing or renaming a Heartbeat field without updating the
//     allowlist also fails here (stale allowlist entry).
//
// If you came here because you're adding a new field: think hard
// about whether it could ever leak PII, hostname, IP, tenant ID, or
// any text-derived value. If yes, do NOT add it — telemetry is
// strictly counter-shaped. If no, add the JSON key to both
// HeartbeatAllowedFields AND the README "Telemetry" section.
func TestHeartbeatFieldAllowlist(t *testing.T) {
	hb := Heartbeat{
		InstallID:     "00000000-0000-4000-8000-000000000000",
		Version:       "test",
		BuildTag:      "default",
		OS:            "linux",
		Arch:          "amd64",
		Backend:       "patterns",
		UptimeSeconds: 1,
		RequestCount:  1,
		ErrorCount:    0,
		EntityCounts:  map[string]uint64{"PERSON": 1},
		P95LatencyMs:  1.23,
	}
	raw, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal heartbeat: %v", err)
	}

	got := make(map[string]struct{}, len(parsed))
	for k := range parsed {
		got[k] = struct{}{}
	}

	// Extra keys in the marshalled output that aren't in the allowlist.
	for k := range got {
		if _, ok := HeartbeatAllowedFields[k]; !ok {
			t.Errorf("heartbeat emits %q which is NOT in HeartbeatAllowedFields. "+
				"If this field is intentional, add it to the allowlist AND to the "+
				"README \"Telemetry\" section in the same commit.", k)
		}
	}
	// Stale allowlist entries (declared allowed but no struct field).
	for k := range HeartbeatAllowedFields {
		if _, ok := got[k]; !ok {
			t.Errorf("HeartbeatAllowedFields lists %q but the Heartbeat struct doesn't emit it. "+
				"Remove the stale entry from HeartbeatAllowedFields.", k)
		}
	}
}

func TestCollectorAggregates(t *testing.T) {
	c := NewCollector()
	c.RecordRequest(0.010, "ok")
	c.RecordRequest(0.020, "ok")
	c.RecordRequest(0.050, "error")
	c.RecordRequest(0.005, "denied") // denied is NOT an error
	c.RecordEntity("PERSON")
	c.RecordEntity("PERSON")
	c.RecordEntity("EMAIL_ADDRESS")
	c.RecordEntity("") // ignored

	hb := c.snapshot(time.Now())
	if hb.RequestCount != 4 {
		t.Errorf("RequestCount: got %d want 4", hb.RequestCount)
	}
	if hb.ErrorCount != 1 {
		t.Errorf("ErrorCount: got %d want 1", hb.ErrorCount)
	}
	if hb.EntityCounts["PERSON"] != 2 {
		t.Errorf("EntityCounts[PERSON]: got %d want 2", hb.EntityCounts["PERSON"])
	}
	if hb.EntityCounts["EMAIL_ADDRESS"] != 1 {
		t.Errorf("EntityCounts[EMAIL_ADDRESS]: got %d want 1", hb.EntityCounts["EMAIL_ADDRESS"])
	}
	if _, leaked := hb.EntityCounts[""]; leaked {
		t.Errorf("empty entity type leaked into EntityCounts")
	}
	if hb.P95LatencyMs <= 0 {
		t.Errorf("P95LatencyMs: got %f want > 0", hb.P95LatencyMs)
	}
}

func TestCollectorResetsOnSnapshot(t *testing.T) {
	c := NewCollector()
	c.RecordRequest(0.010, "ok")
	c.RecordEntity("PERSON")
	_ = c.snapshot(time.Now())
	hb2 := c.snapshot(time.Now())
	if hb2.RequestCount != 0 || hb2.ErrorCount != 0 || len(hb2.EntityCounts) != 0 || hb2.P95LatencyMs != 0 {
		t.Errorf("snapshot did not reset counters: %+v", hb2)
	}
}

func TestCollectorP95Math(t *testing.T) {
	c := NewCollector()
	// 100 evenly-spaced latencies from 1ms to 100ms; p95 = 95ms.
	for i := 1; i <= 100; i++ {
		c.RecordRequest(float64(i)/1000.0, "ok")
	}
	hb := c.snapshot(time.Now())
	if hb.P95LatencyMs < 94 || hb.P95LatencyMs > 96 {
		t.Errorf("P95LatencyMs: got %f want ~95", hb.P95LatencyMs)
	}
}

func TestWrapRecorderTeesObservations(t *testing.T) {
	collector := NewCollector()
	inner := metrics.NewNoop()
	wrapped := WrapRecorder(inner, collector)

	span := wrapped.Request("ingest")
	span.BytesIn(10)
	span.BytesOut(20)
	span.Done("ok")
	wrapped.EntityDetected("PERSON", "GLiNERRecognizer", 0.9)
	wrapped.EntityDetected("EMAIL_ADDRESS", "EmailRecognizer", 0.99)

	hb := collector.snapshot(time.Now())
	if hb.RequestCount != 1 {
		t.Errorf("RequestCount: got %d want 1", hb.RequestCount)
	}
	if hb.EntityCounts["PERSON"] != 1 || hb.EntityCounts["EMAIL_ADDRESS"] != 1 {
		t.Errorf("EntityCounts wrong: %v", hb.EntityCounts)
	}
}

func TestWrapRecorderNoCollectorDegrades(t *testing.T) {
	inner := metrics.NewNoop()
	got := WrapRecorder(inner, nil)
	// Same value — wrap with nil collector returns the inner recorder
	// unchanged, not a wrapper.
	if reflect.TypeOf(got).String() == "*telemetry.teeRecorder" {
		t.Errorf("WrapRecorder(inner, nil) installed a tee; should have returned inner unchanged")
	}
}

func TestInstallIDPersistsAcrossReads(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp) // also redirect home in case fallback is needed

	id1, dir1, err := LoadOrCreateInstallID()
	if err != nil {
		t.Fatalf("first LoadOrCreateInstallID: %v", err)
	}
	if id1 == "" {
		t.Fatalf("first id is empty")
	}
	if dir1 == "" {
		t.Fatalf("first dir is empty (expected XDG path)")
	}
	if _, err := os.Stat(filepath.Join(dir1, InstallIDFile)); err != nil {
		t.Fatalf("install_id file not persisted: %v", err)
	}
	id2, dir2, err := LoadOrCreateInstallID()
	if err != nil {
		t.Fatalf("second LoadOrCreateInstallID: %v", err)
	}
	if id2 != id1 {
		t.Errorf("install_id rolled across reads: %q vs %q", id1, id2)
	}
	if dir2 != dir1 {
		t.Errorf("dir rolled across reads: %q vs %q", dir1, dir2)
	}
}

func TestInstallIDIsUUIDv4Shape(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	id, _, err := LoadOrCreateInstallID()
	if err != nil {
		t.Fatalf("LoadOrCreateInstallID: %v", err)
	}
	// xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx where y is one of 8,9,a,b.
	if len(id) != 36 {
		t.Fatalf("id length: got %d want 36 (id=%q)", len(id), id)
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("id missing UUID dashes: %q", id)
	}
	if id[14] != '4' {
		t.Errorf("id version nibble: got %q want '4' (id=%q)", string(id[14]), id)
	}
	switch id[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("id variant nibble: got %q want one of 8/9/a/b (id=%q)", string(id[19]), id)
	}
}

func TestStartDisabledIsNoOp(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	stop := Start(context.Background(), Config{
		Enabled:   false,
		Endpoint:  srv.URL,
		Collector: NewCollector(),
	})
	defer stop()
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("disabled telemetry posted %d heartbeats; want 0", hits.Load())
	}
}

func TestStartSendsBootHeartbeat(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)

	receivedBody := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type: got %q want application/json", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		select {
		case receivedBody <- body:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	id, dir, err := LoadOrCreateInstallID()
	if err != nil {
		t.Fatalf("install id: %v", err)
	}
	c := NewCollector()
	c.RecordRequest(0.01, "ok")
	c.RecordEntity("PERSON")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stop := Start(ctx, Config{
		Enabled:   true,
		Endpoint:  srv.URL,
		Collector: c,
		DataDir:   dir,
		Static: StaticInfo{
			InstallID: id,
			Version:   "test",
			BuildTag:  "default",
			Backend:   "patterns",
		},
		HTTPClient: srv.Client(),
	})
	defer stop()

	select {
	case body := <-receivedBody:
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		// All emitted keys must be in the allowlist; this is a
		// second line of defence on top of the gate test above.
		for k := range parsed {
			if _, ok := HeartbeatAllowedFields[k]; !ok {
				t.Errorf("wire payload contains forbidden key %q", k)
			}
		}
		if parsed["install_id"] != id {
			t.Errorf("install_id: got %v want %v", parsed["install_id"], id)
		}
		// last_heartbeat is written by the sender goroutine after
		// the POST returns; poll briefly to avoid racing it.
		hbPath := filepath.Join(dir, LastHeartbeatFile)
		deadline := time.Now().Add(1 * time.Second)
		for {
			if _, statErr := os.Stat(hbPath); statErr == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("last_heartbeat not persisted within 1s")
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for boot heartbeat")
	}
}

func TestStartSkipsBootWhenLastHeartbeatRecent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	id, dir, err := LoadOrCreateInstallID()
	if err != nil {
		t.Fatalf("install id: %v", err)
	}
	// Pretend we sent a heartbeat 1 minute ago — well inside the
	// 24h window, so the boot send should be skipped.
	recent := time.Now().Add(-1 * time.Minute).UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(dir, LastHeartbeatFile), []byte(recent), 0o600); err != nil {
		t.Fatalf("seed last_heartbeat: %v", err)
	}

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stop := Start(context.Background(), Config{
		Enabled:   true,
		Endpoint:  srv.URL,
		Collector: NewCollector(),
		DataDir:   dir,
		Static:    StaticInfo{InstallID: id},
	})
	defer stop()
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("sender posted boot heartbeat despite recent last_heartbeat; hits=%d", hits.Load())
	}
}

// Keep the linter happy if sort goes unused after edits.
var _ = sort.Float64s
