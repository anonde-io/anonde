package store

import (
	"context"
	"fmt"
	"sync"
	"time"

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
// TTL correctness: each entry carries an expiry stamped from the vault's
// TTL (see NewCachedVaultWithTTL); an expired hit is a miss, so a warm
// cache can't outlive the vault TTL. TTL=0 means no expiry.
//
// Concurrency: the LRU is goroutine-safe. We add a small RWMutex
// around the Delete-then-Delete sequence to keep the ordering
// guarantee under concurrent Get calls.
type CachedVault struct {
	underlying core.Vault
	cache      *lru.Cache[string, cachedEntry]
	ttl        time.Duration
	mu         sync.RWMutex
}

// cachedEntry pairs a vault value with the wall-clock instant it must no
// longer be served from cache. A zero expiresAt means "never expires"
// (TTL=0).
type cachedEntry struct {
	entry     core.VaultEntry
	expiresAt time.Time
}

func (e cachedEntry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}

// NewCachedVault wraps v with an LRU cache of the given size and no
// cache-side expiry (TTL=0). Kept for callers that don't run a vault
// TTL; NewCachedVaultWithTTL is the TTL-aware form.
func NewCachedVault(v core.Vault, size int) core.Vault {
	return NewCachedVaultWithTTL(v, size, 0)
}

// NewCachedVaultWithTTL wraps v with an LRU cache of the given size,
// expiring cached entries after ttl so a hit can never outlive the
// underlying vault's TTL. Pass the same ttl the underlying vault was
// built with. size=0 returns the underlying vault unchanged; size<0 is a
// programmer error surfaced via panic at startup (callers should
// validate env input).
func NewCachedVaultWithTTL(v core.Vault, size int, ttl time.Duration) core.Vault {
	if size == 0 {
		return v
	}
	if size < 0 {
		panic(fmt.Sprintf("CachedVault: negative size %d", size))
	}
	c, err := lru.New[string, cachedEntry](size)
	if err != nil {
		// lru.New only errors on size<=0; we've already filtered that.
		// Panic surfaces a programmer error rather than a silent fail.
		panic(fmt.Sprintf("CachedVault: %v", err))
	}
	return &CachedVault{underlying: v, cache: c, ttl: ttl}
}

func (c *CachedVault) Put(ctx context.Context, tenantID string, entry core.VaultEntry) error {
	// Stamp expiry before the underlying write so the cache copy expires
	// at or before the underlying row, never after.
	expiresAt := c.expiryFrom(time.Now())
	// Write-through: underlying first. If it fails, leave the cache
	// untouched so a subsequent Get retries the underlying instead of
	// serving a value the persistent store never accepted.
	if err := c.underlying.Put(ctx, tenantID, entry); err != nil {
		return err
	}
	c.cache.Add(cacheKey(tenantID, entry.Token), cachedEntry{entry: entry, expiresAt: expiresAt})
	return nil
}

func (c *CachedVault) Get(ctx context.Context, tenantID, token string) (core.VaultEntry, error) {
	key := cacheKey(tenantID, token)
	if ce, ok := c.cache.Get(key); ok {
		if !ce.expired(time.Now()) {
			return ce.entry, nil
		}
		// Expired: evict and fall through to the underlying, which applies
		// its own TTL check and returns not-found (and lazily drops it).
		c.cache.Remove(key)
	}
	c.mu.RLock()
	e, err := c.underlying.Get(ctx, tenantID, token)
	c.mu.RUnlock()
	if err != nil {
		return core.VaultEntry{}, err
	}
	c.cache.Add(key, cachedEntry{entry: e, expiresAt: c.expiryFrom(time.Now())})
	return e, nil
}

// expiryFrom returns the cache-entry expiry for a value observed at now,
// or the zero time when no TTL is configured (never expire).
func (c *CachedVault) expiryFrom(now time.Time) time.Time {
	if c.ttl <= 0 {
		return time.Time{}
	}
	return now.Add(c.ttl)
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
