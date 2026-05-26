package core

import "context"

// PolicyAuthorizer gates deanonymization access. Service calls
// AllowDetokenize before every detokenize/reveal; implementations are
// free to inspect actor + purpose, audit, deny, or call out to OPA.
type PolicyAuthorizer interface {
	AllowDetokenize(ctx context.Context, req DetokenizeRequest) error
}

// VaultStats reports approximate occupancy for the metrics surface.
// Bytes is best-effort; backends that can't cheaply compute it return
// -1 so the "unknown" state is visible instead of silently zero. This
// type lives in core (not internal/metrics) to avoid pulling
// prometheus into the public interface signature; the metrics
// package adapts it via an alias.
type VaultStats struct {
	Entries int64
	Bytes   int64
}

// StoreStats; see VaultStats. Same semantics applied to the
// anonymization-record store.
type StoreStats struct {
	Entries int64
	Bytes   int64
}

// Vault stores token -> cleartext mappings locally.
type Vault interface {
	Put(ctx context.Context, tenantID string, entry VaultEntry) error
	Get(ctx context.Context, tenantID, token string) (VaultEntry, error)
	// Delete removes a token mapping. Missing tokens are NOT an error,
	// callers (e.g. DeleteAnonymization) iterate over what the store
	// says and must tolerate races / partial state.
	Delete(ctx context.Context, tenantID, token string) error
	// Stats returns an approximate occupancy snapshot. Implementations
	// must keep this cheap; it is called on every Prometheus scrape
	// (typically every 15s) and synchronous I/O here would multiply
	// scrape latency under load. Backends without a cheap byte count
	// return Bytes=-1.
	Stats() VaultStats
}

// Store persists anonymizations (anonymized content + per-doc token
// offsets) keyed by (tenant_id, id).
type Store interface {
	Put(ctx context.Context, record StoreRecord) error
	Get(ctx context.Context, tenantID, id string) (StoreRecord, error)
	// Delete removes a stored anonymization. Returns existed=true iff
	// the record was present before the call so callers can report a
	// meaningful "did anything happen" signal while keeping the
	// operation itself idempotent.
	Delete(ctx context.Context, tenantID, id string) (existed bool, err error)
	// Stats; see Vault.Stats. Same cheapness contract.
	Stats() StoreStats
}
