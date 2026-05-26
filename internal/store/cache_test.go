package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/anonde-io/anonde/internal/core"
)

// countingVault is a fake core.Vault that counts how many times each
// method gets called, so a test can prove the cache served a hit
// without touching the underlying.
type countingVault struct {
	mu      sync.Mutex
	data    map[string]core.VaultEntry
	puts    atomic.Int64
	gets    atomic.Int64
	deletes atomic.Int64
	failGet bool
}

func newCountingVault() *countingVault {
	return &countingVault{data: map[string]core.VaultEntry{}}
}

func (v *countingVault) Put(_ context.Context, tenantID string, e core.VaultEntry) error {
	v.puts.Add(1)
	v.mu.Lock()
	v.data[tenantID+"\x00"+e.Token] = e
	v.mu.Unlock()
	return nil
}

func (v *countingVault) Get(_ context.Context, tenantID, token string) (core.VaultEntry, error) {
	v.gets.Add(1)
	if v.failGet {
		return core.VaultEntry{}, errors.New("fake underlying error")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.data[tenantID+"\x00"+token]
	if !ok {
		return core.VaultEntry{}, errors.New("not found")
	}
	return e, nil
}

func (v *countingVault) Delete(_ context.Context, tenantID, token string) error {
	v.deletes.Add(1)
	v.mu.Lock()
	delete(v.data, tenantID+"\x00"+token)
	v.mu.Unlock()
	return nil
}

func (v *countingVault) Stats() core.VaultStats {
	v.mu.Lock()
	defer v.mu.Unlock()
	return core.VaultStats{Entries: int64(len(v.data))}
}

// ─── Size zero returns the underlying unchanged ────────────────────

func TestCachedVault_SizeZero_NoWrapper(t *testing.T) {
	under := newCountingVault()
	got := NewCachedVault(under, 0)
	if got != core.Vault(under) {
		t.Fatalf("size=0 should return the underlying vault unchanged")
	}
}

// ─── Second Get is a cache hit (no underlying call) ────────────────

func TestCachedVault_HitsCache(t *testing.T) {
	under := newCountingVault()
	v := NewCachedVault(under, 100)
	ctx := context.Background()
	entry := core.VaultEntry{Token: "<T>", EntityType: "PERSON", Cleartext: "Alice"}
	_ = v.Put(ctx, "demo", entry)

	if _, err := v.Get(ctx, "demo", "<T>"); err != nil {
		t.Fatalf("Get 1: %v", err)
	}
	if _, err := v.Get(ctx, "demo", "<T>"); err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if _, err := v.Get(ctx, "demo", "<T>"); err != nil {
		t.Fatalf("Get 3: %v", err)
	}

	// Put populated the cache; three subsequent Gets should not have
	// reached the underlying at all.
	if got := under.gets.Load(); got != 0 {
		t.Fatalf("expected 0 underlying Get calls after Put-then-3Gets, got %d", got)
	}
}

// ─── Cold Get populates cache from underlying ──────────────────────

func TestCachedVault_PopulatesOnMiss(t *testing.T) {
	under := newCountingVault()
	// Seed underlying without touching cache.
	_ = under.Put(context.Background(), "demo", core.VaultEntry{Token: "<T>", EntityType: "PERSON", Cleartext: "Alice"})

	v := NewCachedVault(under, 100)
	ctx := context.Background()

	if _, err := v.Get(ctx, "demo", "<T>"); err != nil {
		t.Fatalf("Get cold: %v", err)
	}
	if _, err := v.Get(ctx, "demo", "<T>"); err != nil {
		t.Fatalf("Get warm: %v", err)
	}

	// Two Gets, only one should have hit underlying (the cold one).
	if got := under.gets.Load(); got != 1 {
		t.Fatalf("expected 1 underlying Get (cold), got %d", got)
	}
}

// ─── Delete invalidates the cache ──────────────────────────────────

func TestCachedVault_DeleteInvalidates(t *testing.T) {
	under := newCountingVault()
	v := NewCachedVault(under, 100)
	ctx := context.Background()
	entry := core.VaultEntry{Token: "<T>", EntityType: "PERSON", Cleartext: "Alice"}
	_ = v.Put(ctx, "demo", entry)
	_, _ = v.Get(ctx, "demo", "<T>") // warm cache (0 underlying Gets so far)

	if err := v.Delete(ctx, "demo", "<T>"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := v.Get(ctx, "demo", "<T>"); err == nil {
		t.Fatalf("Get after Delete: expected error, got nil; cache served stale entry")
	}
	// Get-after-Delete must reach the underlying (one Get).
	if got := under.gets.Load(); got != 1 {
		t.Fatalf("expected 1 underlying Get after delete, got %d", got)
	}
}

// ─── Failing underlying Put leaves cache unchanged ─────────────────

// errPutVault always fails Put; verifies write-through ordering: the
// cache must not learn about a Put the underlying rejected.
type errPutVault struct{ *countingVault }

func (v *errPutVault) Put(_ context.Context, _ string, _ core.VaultEntry) error {
	v.puts.Add(1)
	return errors.New("underlying put failed")
}

func TestCachedVault_PutFailureDoesNotPoisonCache(t *testing.T) {
	under := &errPutVault{countingVault: newCountingVault()}
	v := NewCachedVault(under, 100)
	ctx := context.Background()
	entry := core.VaultEntry{Token: "<T>", EntityType: "PERSON", Cleartext: "Alice"}

	if err := v.Put(ctx, "demo", entry); err == nil {
		t.Fatalf("expected Put error from underlying")
	}
	// A subsequent Get should hit the underlying (which still has no
	// data), not the cache.
	_, err := v.Get(ctx, "demo", "<T>")
	if err == nil {
		t.Fatalf("expected not-found after failed Put, got cached value")
	}
	if got := under.gets.Load(); got != 1 {
		t.Fatalf("expected 1 underlying Get after failed Put, got %d", got)
	}
}

// ─── Underlying error on Get is not cached ─────────────────────────

func TestCachedVault_ErrorsAreNotCached(t *testing.T) {
	under := newCountingVault()
	under.failGet = true
	v := NewCachedVault(under, 100)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := v.Get(ctx, "demo", "<T>"); err == nil {
			t.Fatalf("iteration %d: expected underlying error", i)
		}
	}
	// All 3 Gets should have hit the underlying; errors are not cached.
	if got := under.gets.Load(); got != 3 {
		t.Fatalf("expected 3 underlying Gets when errors are not cached, got %d", got)
	}
}
