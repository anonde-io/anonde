package store

import (
	"context"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/anonde-io/anonde/internal/core"
)

// CachedVault is a read-through LRU cache in front of any core.Vault.
//
// Why only the Vault, not the Store: a reveal call does N vault
// lookups (one per token in the document) but exactly one Store.Get.
// The read amplification; and therefore the I/O cost worth caching
// away; is all on the vault side. Store records also carry the full
// anonymized blob and can be 100 KB+; caching N of those would be a
// memory-bound footgun. Vault entries are ~200 bytes each.
//
// Cache semantics:
//
//   - Get: cache hit returns immediately; miss reads through and
//     populates on success. Errors are NOT cached (a transient bbolt
//     error shouldn't poison subsequent reads).
//   - Put: writes through to the underlying first, only updates the
//     cache on success. Order matters; a write that fails the
//     underlying must not leave a stale-but-acknowledged entry in
//     the cache.
//   - Delete: removes the cache entry first, then deletes underlying.
//     Order matters the other way here: if the cache delete is the
//     last step, a racing Get between underlying-delete and
//     cache-delete would serve stale plaintext.
//
// This wrapper is correct for TTL=0 (no expiry, current default).
// With non-zero TTL, the underlying expires rows but the cache
// doesn't; a tradeoff documented at NewCachedVault. Memory backend
// users don't need this wrapper.
//
// Concurrency: the LRU is goroutine-safe. We add a small RWMutex
// around the Delete-then-Delete sequence to keep the ordering
// guarantee under concurrent Get calls.
type CachedVault struct {
	underlying core.Vault
	cache      *lru.Cache[string, core.VaultEntry]
	mu         sync.RWMutex
}

// NewCachedVault wraps v with an LRU cache of the given size. size=0
// returns the underlying vault unchanged; size<0 is a programmer error
// surfaced via panic at startup (callers should validate env input).
//
// Caveat with TTL: if the underlying has TTL>0, an entry can expire
// in the underlying while still sitting in this cache. The cached
// hit would serve stale plaintext until LRU eviction takes it. For
// strict TTL correctness with a cache, the underlying TTL should
// match a cache-side TTL (not implemented; today's default is TTL=0
// so this is a non-issue).
func NewCachedVault(v core.Vault, size int) core.Vault {
	if size == 0 {
		return v
	}
	if size < 0 {
		panic(fmt.Sprintf("CachedVault: negative size %d", size))
	}
	c, err := lru.New[string, core.VaultEntry](size)
	if err != nil {
		// lru.New only errors on size<=0; we've already filtered that.
		// Panic surfaces a programmer error rather than a silent fail.
		panic(fmt.Sprintf("CachedVault: %v", err))
	}
	return &CachedVault{underlying: v, cache: c}
}

func (c *CachedVault) Put(ctx context.Context, tenantID string, entry core.VaultEntry) error {
	// Write-through: underlying first. If it fails, leave the cache
	// untouched so a subsequent Get retries the underlying instead of
	// serving a value the persistent store never accepted.
	if err := c.underlying.Put(ctx, tenantID, entry); err != nil {
		return err
	}
	c.cache.Add(cacheKey(tenantID, entry.Token), entry)
	return nil
}

func (c *CachedVault) Get(ctx context.Context, tenantID, token string) (core.VaultEntry, error) {
	key := cacheKey(tenantID, token)
	if e, ok := c.cache.Get(key); ok {
		return e, nil
	}
	c.mu.RLock()
	e, err := c.underlying.Get(ctx, tenantID, token)
	c.mu.RUnlock()
	if err != nil {
		return core.VaultEntry{}, err
	}
	c.cache.Add(key, e)
	return e, nil
}

// Stats forwards to the underlying. The cache itself does not add
// entries to the vault, it only mirrors a subset of them, so the
// underlying's count is the authoritative value to publish.
func (c *CachedVault) Stats() core.VaultStats { return c.underlying.Stats() }

func (c *CachedVault) Delete(ctx context.Context, tenantID, token string) error {
	key := cacheKey(tenantID, token)
	// Remove from cache first so concurrent Gets don't see a stale
	// entry while the underlying delete is still in flight. Under the
	// write lock so that a Get can't repopulate from the underlying
	// between our two operations.
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Remove(key)
	return c.underlying.Delete(ctx, tenantID, token)
}

func cacheKey(tenantID, token string) string {
	return tenantID + "\x00" + token
}

var _ core.Vault = (*CachedVault)(nil)
