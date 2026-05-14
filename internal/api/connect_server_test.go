package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	platformv1 "github.com/anonde-io/anonde/gen/anonde/platform/v1"
	"github.com/anonde-io/anonde/gen/anonde/platform/v1/platformv1connect"
	"github.com/anonde-io/anonde/internal/core"
)

// newConnectTestEnv spins up an httptest server backed by the same
// service the unit tests use, and returns a Connect client pointed at
// it. Used to verify the proto-handler wiring end-to-end without
// having to hand-write JSON bodies for every test.
func newConnectTestEnv(t *testing.T) (platformv1connect.PlatformServiceClient, *core.Service) {
	t.Helper()
	svc := newTestService()
	api := NewHTTPServer(svc)
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(srv.Close)
	return platformv1connect.NewPlatformServiceClient(srv.Client(), srv.URL), svc
}

func TestConnect_HealthCheck(t *testing.T) {
	client, _ := newConnectTestEnv(t)
	resp, err := client.HealthCheck(context.Background(), connect.NewRequest(&platformv1.HealthCheckRequest{}))
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if resp.Msg.GetStatus() != platformv1.HealthCheckResponse_SERVING_STATUS_SERVING {
		t.Fatalf("expected SERVING, got %v", resp.Msg.GetStatus())
	}
}

func TestConnect_GetVersion_ReturnsStampedInfo(t *testing.T) {
	client, svc := newConnectTestEnv(t)
	svc.SetVersionInfo(core.VersionInfo{
		AnalyzerBackend: "patterns",
		Model:           "",
		BuildSHA:        "deadbeef",
		GoVersion:       "go1.99",
		APIVersion:      "v1",
	})
	resp, err := client.GetVersion(context.Background(), connect.NewRequest(&platformv1.GetVersionRequest{}))
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if resp.Msg.GetAnalyzerBackend() != "patterns" {
		t.Fatalf("unexpected backend: %q", resp.Msg.GetAnalyzerBackend())
	}
	if resp.Msg.GetBuildSha() != "deadbeef" {
		t.Fatalf("unexpected build sha: %q", resp.Msg.GetBuildSha())
	}
	if resp.Msg.GetApiVersion() != "v1" {
		t.Fatalf("unexpected api version: %q", resp.Msg.GetApiVersion())
	}
}

// TestConnect_CreateAnonymization_MintsIDWhenEmpty verifies the
// hybrid-id contract: caller omits `id` → server mints one prefixed
// `anon_`, returns it, and subsequent reveal/delete using that id
// must work. This is the same shape Stripe uses for `ch_xxx` etc.
func TestConnect_CreateAnonymization_MintsIDWhenEmpty(t *testing.T) {
	client, _ := newConnectTestEnv(t)
	ctx := context.Background()

	resp, err := client.CreateAnonymization(ctx, connect.NewRequest(&platformv1.CreateAnonymizationRequest{
		TenantId:      "acme",
		// Id deliberately omitted.
		ContentFormat: "text",
		Content:       "Email alice@example.com",
	}))
	if err != nil {
		t.Fatalf("CreateAnonymization: %v", err)
	}
	mintedID := resp.Msg.GetId()
	if !strings.HasPrefix(mintedID, "anon_") {
		t.Fatalf("expected minted id to be prefixed `anon_`, got %q", mintedID)
	}
	if len(mintedID) != len("anon_")+16 {
		t.Fatalf("expected minted id length %d, got %d (%q)", len("anon_")+16, len(mintedID), mintedID)
	}

	// Round-trip the minted ID through reveal — the only ID we hold
	// is the one the server returned.
	rev, err := client.RevealContent(ctx, connect.NewRequest(&platformv1.RevealContentRequest{
		TenantId:      "acme",
		Id:            mintedID,
		Actor:         "tester",
		Purpose:       "verify",
		ContentFormat: "text",
		Content:       resp.Msg.GetAnonymizedContent(),
	}))
	if err != nil {
		t.Fatalf("RevealContent against minted id: %v", err)
	}
	if !strings.Contains(rev.Msg.GetDeanonymizedContent(), "alice@example.com") {
		t.Fatalf("expected email restored after reveal-by-minted-id, got %q", rev.Msg.GetDeanonymizedContent())
	}
}

// TestConnect_IngestRevealDelete_RoundTrip exercises the full
// ingest → reveal → delete cycle through the generated client. The
// goal isn't to re-test the analyzer (already covered) but to verify
// proto field mapping is correct end-to-end: tenant/doc/content go in,
// tokens come back, reveal restores cleartext, delete drops everything.
func TestConnect_IngestRevealDelete_RoundTrip(t *testing.T) {
	client, _ := newConnectTestEnv(t)
	ctx := context.Background()

	ing, err := client.CreateAnonymization(ctx, connect.NewRequest(&platformv1.CreateAnonymizationRequest{
		TenantId:      "acme",
		Id:         "letter-001",
		ContentFormat: "text",
		Content:       "Email alice@example.com about the case",
	}))
	if err != nil {
		t.Fatalf("CreateAnonymization: %v", err)
	}
	if strings.Contains(ing.Msg.GetAnonymizedContent(), "alice@example.com") {
		t.Fatalf("expected email anonymized, got %q", ing.Msg.GetAnonymizedContent())
	}
	if len(ing.Msg.GetTokens()) == 0 {
		t.Fatalf("expected at least one token")
	}

	rev, err := client.RevealContent(ctx, connect.NewRequest(&platformv1.RevealContentRequest{
		TenantId:      "acme",
		Id:         "letter-001",
		Actor:         "test",
		Purpose:       "verify",
		ContentFormat: "text",
		Content:       ing.Msg.GetAnonymizedContent(),
	}))
	if err != nil {
		t.Fatalf("RevealContent: %v", err)
	}
	if !strings.Contains(rev.Msg.GetDeanonymizedContent(), "alice@example.com") {
		t.Fatalf("expected email restored, got %q", rev.Msg.GetDeanonymizedContent())
	}

	del, err := client.DeleteAnonymization(ctx, connect.NewRequest(&platformv1.DeleteAnonymizationRequest{
		TenantId: "acme",
		Id:    "letter-001",
	}))
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if !del.Msg.GetDeleted() {
		t.Fatalf("expected deleted=true")
	}
	if del.Msg.GetTokensDeleted() == 0 {
		t.Fatalf("expected tokens_deleted > 0")
	}

	// After deletion, reveal must fail with NotFound-equivalent — Service
	// returns an error from store.Get, which connectErrFor maps to
	// InvalidArgument (no specific NotFound mapping yet). Asserting on
	// the error string keeps the test stable across that future mapping.
	_, err = client.RevealContent(ctx, connect.NewRequest(&platformv1.RevealContentRequest{
		TenantId:      "acme",
		Id:         "letter-001",
		Actor:         "test",
		Purpose:       "verify",
		ContentFormat: "text",
		Content:       ing.Msg.GetAnonymizedContent(),
	}))
	if err == nil {
		t.Fatalf("expected error after delete, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// TestConnect_DeleteDocument_IdempotentForMissingDoc verifies the
// "idempotent OK" contract: deleting a doc that never existed returns
// OK with deleted=false, tokens_deleted=0.
func TestConnect_DeleteDocument_IdempotentForMissingDoc(t *testing.T) {
	client, _ := newConnectTestEnv(t)
	resp, err := client.DeleteAnonymization(context.Background(), connect.NewRequest(&platformv1.DeleteAnonymizationRequest{
		TenantId: "acme",
		Id:    "never-existed",
	}))
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if resp.Msg.GetDeleted() {
		t.Fatalf("expected deleted=false for missing doc")
	}
	if resp.Msg.GetTokensDeleted() != 0 {
		t.Fatalf("expected tokens_deleted=0, got %d", resp.Msg.GetTokensDeleted())
	}
}
