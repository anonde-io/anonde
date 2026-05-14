// Package core owns the platform's business logic: the Service type
// that orchestrates analyze → tokenize → vault → reveal, the internal
// request/response DTOs the transport layers convert proto messages
// to/from, and the Vault/Store/PolicyAuthorizer interfaces that wire
// in pluggable storage and authz backends.
//
// core has no dependency on the api layer (Connect, gRPC, gateway,
// HTTP server) and no dependency on a specific Store/Vault impl. The
// store package depends on core for the interface definitions and
// shared data types; the api package depends on core for Service and
// the DTOs. cmd/platform wires them all together.
package core

import "github.com/anonde-io/anonde/analyzer"

// IngestRequest creates a new anonymization. ID is optional — if
// empty, the service mints one (prefixed `anon_`) and returns it in
// IngestResponse.
type IngestRequest struct {
	TenantID      string `json:"tenant_id"`
	ID            string `json:"id,omitempty"`
	Content       string `json:"content"`
	ContentFormat string `json:"content_format,omitempty"`

	// Optional analyzer overrides. Empty/zero means use service defaults.
	Language       string   `json:"language,omitempty"`
	Entities       []string `json:"entities,omitempty"`
	ScoreThreshold float64  `json:"score_threshold,omitempty"`
	DisableNER     bool     `json:"disable_ner,omitempty"`
}

type IngestResponse struct {
	TenantID           string                      `json:"tenant_id"`
	ID                 string                      `json:"id"`
	AnonymizedContent  string                      `json:"anonymized_content"`
	DetectedEntitySize int                         `json:"detected_entity_size"`
	Findings           []analyzer.RecognizerResult `json:"findings"`
	Tokens             []TokenRef                  `json:"tokens"`
}

type TokenRef struct {
	Token      string `json:"token"`
	EntityType string `json:"entity_type"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
}

// StoreRecord is the persisted anonymization: the anonymized blob plus
// the token offsets needed to reveal it.
type StoreRecord struct {
	TenantID          string     `json:"tenant_id"`
	ID                string     `json:"id"`
	ContentFormat     string     `json:"content_format,omitempty"`
	AnonymizedContent string     `json:"anonymized_content"`
	Tokens            []TokenRef `json:"tokens"`
}

// VaultEntry stores one token mapping.
type VaultEntry struct {
	Token      string
	EntityType string
	Cleartext  string
}

type DetokenizeRequest struct {
	TenantID string   `json:"tenant_id"`
	ID       string   `json:"id"`
	Actor    string   `json:"actor"`
	Purpose  string   `json:"purpose"`
	Tokens   []string `json:"tokens"`
}

type DetokenizeResponse struct {
	TenantID string            `json:"tenant_id"`
	ID       string            `json:"id"`
	Resolved map[string]string `json:"resolved"`
}

type RevealRequest struct {
	TenantID      string `json:"tenant_id"`
	ID            string `json:"id"`
	Actor         string `json:"actor"`
	Purpose       string `json:"purpose"`
	Content       string `json:"content"`
	ContentFormat string `json:"content_format,omitempty"`
}

type RevealResponse struct {
	TenantID            string            `json:"tenant_id"`
	ID                  string            `json:"id"`
	DeanonymizedContent string            `json:"deanonymized_content"`
	Resolved            map[string]string `json:"resolved"`
}

type SynthesizeRequest struct {
	Content        string   `json:"content"`
	ContentFormat  string   `json:"content_format,omitempty"`
	Language       string   `json:"language,omitempty"`
	Entities       []string `json:"entities,omitempty"`
	ScoreThreshold float64  `json:"score_threshold,omitempty"`
	DisableNER     bool     `json:"disable_ner,omitempty"`
	// Consistent=true means the same input text always produces the same fake.
	Consistent bool `json:"consistent,omitempty"`
	// DocScoped=true (requires Consistent) means the same text maps to the same
	// fake within this single request only.
	DocScoped bool `json:"doc_scoped,omitempty"`
}

type SynthesizeResponse struct {
	Content  string                      `json:"content"`
	Findings []analyzer.RecognizerResult `json:"findings"`
}

// VersionInfo is populated by the binary entrypoint (cmd/platform) and
// served back by GetVersion. The service layer doesn't introspect the
// analyzer because the backend selection lives at main wiring time.
type VersionInfo struct {
	AnalyzerBackend string
	Model           string
	BuildSHA        string
	GoVersion       string
	APIVersion      string
}

// DeleteResult reports what DeleteAnonymization actually did. The RPC
// itself is idempotent OK, but callers may want to distinguish
// "nothing was here" from "we cleaned up N tokens" for metrics / audit
// purposes.
type DeleteResult struct {
	Deleted       bool
	TokensDeleted int
}
