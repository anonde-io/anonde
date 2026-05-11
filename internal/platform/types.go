package platform

import "github.com/moogacs/anonde/analyzer"

type IngestRequest struct {
	TenantID      string `json:"tenant_id"`
	DocID         string `json:"doc_id"`
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
	DocID              string                      `json:"doc_id"`
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

type StoreRecord struct {
	TenantID          string     `json:"tenant_id"`
	DocID             string     `json:"doc_id"`
	ContentFormat     string     `json:"content_format,omitempty"`
	AnonymizedContent string     `json:"anonymized_content"`
	Tokens            []TokenRef `json:"tokens"`
}

type DetokenizeRequest struct {
	TenantID string   `json:"tenant_id"`
	DocID    string   `json:"doc_id"`
	Actor    string   `json:"actor"`
	Purpose  string   `json:"purpose"`
	Tokens   []string `json:"tokens"`
}

type DetokenizeResponse struct {
	TenantID string            `json:"tenant_id"`
	DocID    string            `json:"doc_id"`
	Resolved map[string]string `json:"resolved"`
}

type RevealRequest struct {
	TenantID      string `json:"tenant_id"`
	DocID         string `json:"doc_id"`
	Actor         string `json:"actor"`
	Purpose       string `json:"purpose"`
	Content       string `json:"content"`
	ContentFormat string `json:"content_format,omitempty"`
}

type RevealResponse struct {
	TenantID            string            `json:"tenant_id"`
	DocID               string            `json:"doc_id"`
	DeanonymizedContent string            `json:"deanonymized_content"`
	Resolved            map[string]string `json:"resolved"`
}

type SynthesizeRequest struct {
	Content       string   `json:"content"`
	ContentFormat string   `json:"content_format,omitempty"`
	Language      string   `json:"language,omitempty"`
	Entities      []string `json:"entities,omitempty"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
	DisableNER    bool     `json:"disable_ner,omitempty"`
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
