package store

import (
	"context"
	"testing"
	"time"

	"github.com/anonde-io/anonde/internal/core"
)

// cacheTestTenant is declared once and referenced by identifier at every
// Put/Get site so both halves of a round-trip use byte-identical tenant
// bytes (a bare string literal repeated at two sites is not guaranteed
// to survive tooling identically).
const cacheTestTenant = "tnt-cache"

// TestCachedVault_RespectsTTL is the regression for "cached bbolt vault
// bypasses vault TTL": with a warm cache, a value must stop being
// revealable once the vault TTL has expired the underlying row — the
// cache must not keep serving stale plaintext until LRU eviction.
func TestCachedVault_RespectsTTL(t *testing.T) {
	// TTL comfortably larger than a bbolt fsync so the warm Get is
	// reliable; the post-sleep Get is well past expiry.
	const ttl = 300 * time.Millisecond
	under, _ := newTestBoltVault(t, ttl) // real underlying with the same TTL
	v := NewCachedVaultWithTTL(under, 100, ttl)
	ctx := context.Background()

	entry := core.VaultEntry{Token: "tok-a-1", EntityType: "TYPE_A", Cleartext: "cleartext-value-1"}
	if err := v.Put(ctx, cacheTestTenant, entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Warm the cache so this exercises the cache path, not the underlying.
	if _, err := v.Get(ctx, cacheTestTenant, entry.Token); err != nil {
		t.Fatalf("warm Get: %v", err)
	}

	time.Sleep(ttl + 400*time.Millisecond)

	if got, err := v.Get(ctx, cacheTestTenant, entry.Token); err == nil {
		t.Fatalf("expected not-found after TTL, cache served stale entry %+v", got)
	}
}

// TestCachedVault_NoTTLNeverExpires guards the default (TTL=0) path: a
// cached hit stays valid indefinitely, matching a no-expiry underlying,
// and is served from cache (no underlying read).
func TestCachedVault_NoTTLNeverExpires(t *testing.T) {
	under := newCountingVault()
	v := NewCachedVaultWithTTL(under, 100, 0)
	ctx := context.Background()
	entry := core.VaultEntry{Token: "tok-b-1", EntityType: "TYPE_B", Cleartext: "cleartext-value-2"}
	if err := v.Put(ctx, cacheTestTenant, entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	got, err := v.Get(ctx, cacheTestTenant, entry.Token)
	if err != nil {
		t.Fatalf("TTL=0 Get: %v", err)
	}
	if got != entry {
		t.Fatalf("TTL=0 mismatch: got %+v", got)
	}
	// Served from cache: the underlying was never read.
	if n := under.gets.Load(); n != 0 {
		t.Fatalf("expected 0 underlying Gets with warm TTL=0 cache, got %d", n)
	}
}
