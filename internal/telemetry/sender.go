package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultEndpoint is the production telemetry sink. Operators can
// override with ANONDE_TELEMETRY_URL; setting it to the empty string
// is equivalent to ANONDE_TELEMETRY=off.
const DefaultEndpoint = "https://telemetry.anonde.io/v1/heartbeat"

// HeartbeatInterval is how often the sender ticks once running.
const HeartbeatInterval = 24 * time.Hour

// sendTimeout caps a single heartbeat POST. Telemetry must never
// stall on a slow network; failure is silent (logged at info level
// at most).
const sendTimeout = 10 * time.Second

// StaticInfo is the slice of the heartbeat the sender can fill in
// from environment + build info alone, with no observation needed.
// Built once at boot and reused on every heartbeat.
type StaticInfo struct {
	InstallID string
	Version   string
	BuildTag  string
	Backend   string
}

// Config bundles the wiring a Sender needs. cmd/anonde/main owns
// resolution from env vars; the package itself does no env reads at
// runtime (clearer + easier to test).
type Config struct {
	// Enabled is the result of evaluating ANONDE_TELEMETRY +
	// ANONDE_OFFLINE in main. When false, Start returns a no-op
	// stop func without touching disk.
	Enabled bool

	// Endpoint is the full URL to POST heartbeats to. Empty disables
	// the sender even when Enabled is true.
	Endpoint string

	// Static is the boot-time half of the heartbeat (install ID,
	// version, backend, build tag).
	Static StaticInfo

	// DataDir is the directory used to persist last_heartbeat (next
	// to install_id). Empty disables the persistence side of the
	// "skip boot heartbeat if last <24h" rule, which is fine — the
	// sender falls back to sending on boot.
	DataDir string

	// Collector is the observation accumulator. nil short-circuits
	// Start to a no-op; callers should always provide one when
	// Enabled is true.
	Collector *Collector

	// HTTPClient is the client used to POST heartbeats. nil means
	// "build a sensible default with sendTimeout". Tests inject a
	// stub.
	HTTPClient *http.Client
}

// Start launches the sender goroutine and returns a stop func the
// caller can defer. The goroutine respects ctx cancellation; the
// returned stop func cancels an internal context if the caller
// hasn't provided their own.
//
// On boot the sender checks last_heartbeat: if the last successful
// send was < HeartbeatInterval ago, the boot heartbeat is skipped
// (so a flapping container doesn't pummel the endpoint). Otherwise
// the first heartbeat fires immediately.
//
// Failure is silent: a non-2xx response or network error is logged
// at info level and dropped; the next tick retries.
func Start(ctx context.Context, cfg Config) (stop func()) {
	if !cfg.Enabled || cfg.Endpoint == "" || cfg.Collector == nil {
		return func() {}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}

	innerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		s := &sender{cfg: cfg, client: client}
		s.run(innerCtx)
	}()

	return func() {
		cancel()
		// Bounded wait so a shutdown isn't held hostage by a slow
		// in-flight POST.
		select {
		case <-done:
		case <-time.After(sendTimeout + 1*time.Second):
		}
	}
}

type sender struct {
	cfg    Config
	client *http.Client
}

func (s *sender) run(ctx context.Context) {
	// Boot heartbeat: send unless the persisted last_heartbeat says
	// we already sent one within the window. Always honour ctx
	// cancellation in case the operator shuts down inside the first
	// few seconds.
	if s.shouldSendOnBoot() {
		s.tick(ctx)
	}
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *sender) tick(ctx context.Context) {
	hb := s.cfg.Collector.snapshot(time.Now())
	hb.InstallID = s.cfg.Static.InstallID
	hb.Version = s.cfg.Static.Version
	hb.BuildTag = s.cfg.Static.BuildTag
	hb.Backend = s.cfg.Static.Backend
	hb.OS = runtime.GOOS
	hb.Arch = runtime.GOARCH

	payload, err := json.Marshal(hb)
	if err != nil {
		// Shouldn't happen; Heartbeat is a closed shape with simple
		// types. Log + drop.
		log.Printf("telemetry: marshal heartbeat: %v", err)
		return
	}
	if err := s.post(ctx, payload); err != nil {
		log.Printf("telemetry: send failed (will retry at next tick): %v", err)
		return
	}
	s.recordLastHeartbeat()
}

func (s *sender) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "anonde-telemetry/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	// Drain (so the connection can be reused) but cap at 4 KiB; the
	// Worker should respond with at most "ok" or a small JSON.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("non-2xx status: %s", resp.Status)
	}
	return nil
}

// shouldSendOnBoot returns false when the persisted last_heartbeat
// is younger than HeartbeatInterval. Any I/O error → fall back to
// sending; missing-data is treated as "first boot, send now".
func (s *sender) shouldSendOnBoot() bool {
	if s.cfg.DataDir == "" {
		return true
	}
	raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, LastHeartbeatFile))
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil {
		return true
	}
	return time.Since(last) >= HeartbeatInterval
}

// recordLastHeartbeat persists the most recent send timestamp. Best
// effort: an unwritable data dir just means we'll send on every
// boot, which is fine.
func (s *sender) recordLastHeartbeat() {
	if s.cfg.DataDir == "" {
		return
	}
	path := filepath.Join(s.cfg.DataDir, LastHeartbeatFile)
	_ = os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}
