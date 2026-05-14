package core

import "context"

// PolicyAuthorizer gates deanonymization access. Service calls
// AllowDetokenize before every detokenize/reveal — implementations are
// free to inspect actor + purpose, audit, deny, or call out to OPA.
type PolicyAuthorizer interface {
	AllowDetokenize(ctx context.Context, req DetokenizeRequest) error
}

// Vault stores token -> cleartext mappings locally.
type Vault interface {
	Put(ctx context.Context, tenantID string, entry VaultEntry) error
	Get(ctx context.Context, tenantID, token string) (VaultEntry, error)
	// Delete removes a token mapping. Missing tokens are NOT an error —
	// callers (e.g. DeleteAnonymization) iterate over what the store
	// says and must tolerate races / partial state.
	Delete(ctx context.Context, tenantID, token string) error
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
}
