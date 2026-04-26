package recognizers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/moogacs/anonde/analyzer"
)

const defaultPresidioEndpoint = "http://localhost:3000"

// PresidioRemoteNERRecognizer detects named entities by calling a Python
// Presidio Analyzer HTTP service.
type PresidioRemoteNERRecognizer struct {
	endpoint string
	client   *http.Client
}

// NewPresidioRemoteNERRecognizer creates a recognizer backed by a remote
// Presidio analyzer service. Endpoint defaults to http://localhost:3000.
func NewPresidioRemoteNERRecognizer(endpoint string) *PresidioRemoteNERRecognizer {
	if endpoint == "" {
		endpoint = defaultPresidioEndpoint
	}
	return &PresidioRemoteNERRecognizer{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{},
	}
}

func (r *PresidioRemoteNERRecognizer) Name() string {
	return "PresidioRemoteNERRecognizer"
}

func (r *PresidioRemoteNERRecognizer) SupportedEntities() []string {
	return []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"}
}

func (r *PresidioRemoteNERRecognizer) SupportedLanguages() []string { return []string{"en"} }

type presidioAnalyzeRequest struct {
	Text     string   `json:"text"`
	Language string   `json:"language"`
	Entities []string `json:"entities,omitempty"`
}

type presidioAnalyzeResult struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

func (r *PresidioRemoteNERRecognizer) Analyze(ctx context.Context, text string, entities []string, language string) ([]analyzer.RecognizerResult, error) {
	if language == "" {
		language = "en"
	}

	reqBody := presidioAnalyzeRequest{
		Text:     text,
		Language: language,
	}
	if len(entities) > 0 {
		reqBody.Entities = entities
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("presidio analyzer: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var raw []presidioAnalyzeResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	var out []analyzer.RecognizerResult
	for _, rr := range raw {
		if rr.Start < 0 || rr.End <= rr.Start || rr.End > len(text) {
			continue
		}
		out = append(out, analyzer.RecognizerResult{
			Start:          rr.Start,
			End:            rr.End,
			Score:          rr.Score,
			EntityType:     rr.EntityType,
			RecognizerName: r.Name(),
		})
	}
	return out, nil
}
