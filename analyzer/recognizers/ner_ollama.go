package recognizers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
)

const ollamaNERSystemPrompt = `You are a multilingual named entity recognition system. Extract all named entities from the text in any language (English, German, Spanish, French, Italian, etc.).
Return ONLY a JSON object with no preamble or explanation:
{"entities":[{"text":"exact text as it appears","type":"PERSON|LOCATION|ORGANIZATION|NRP"}]}
Types: PERSON (people, e.g. "John Smith", "Frau Müller"), LOCATION (places, e.g. "Berlin", "Sankt Gallen"), ORGANIZATION (companies/institutions/hospitals, e.g. "Charité", "Acme GmbH"), NRP (nationalities/religions/political groups).
Preserve the exact surface form (including German umlauts and case).
If no entities are found, return {"entities":[]}.`

const maxOllamaChunkBytes = 8000

// OllamaNERRecognizer detects named entities by calling a local Ollama instance.
// All inference runs locally; no data leaves the machine.
type OllamaNERRecognizer struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewOllamaNERRecognizer creates an Ollama-backed NER recognizer.
// endpoint defaults to "http://localhost:11434" if empty.
// model defaults to "phi3:mini" if empty.
func NewOllamaNERRecognizer(endpoint, model string) *OllamaNERRecognizer {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "phi3:mini"
	}
	return &OllamaNERRecognizer{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{},
	}
}

func (r *OllamaNERRecognizer) Name() string { return "NERRecognizer" }
func (r *OllamaNERRecognizer) SupportedEntities() []string {
	return []string{"PERSON", "LOCATION", "ORGANIZATION", "NRP"}
}
// SupportedLanguages declares the languages the multilingual prompt covers.
// The underlying Ollama model must itself be multilingual for non-English to
// work well; the prompt and label scheme are language-neutral.
func (r *OllamaNERRecognizer) SupportedLanguages() []string {
	return []string{"en", "de", "es", "fr", "it", "nl", "pt"}
}

func (r *OllamaNERRecognizer) Analyze(ctx context.Context, text string, entities []string, _ string) ([]analyzer.RecognizerResult, error) {
	wantAll := len(entities) == 0
	want := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		want[e] = struct{}{}
	}

	if len(text) <= maxOllamaChunkBytes {
		return r.analyzeChunk(ctx, text, 0, wantAll, want)
	}
	return r.analyzeChunked(ctx, text, wantAll, want)
}

func (r *OllamaNERRecognizer) analyzeChunked(ctx context.Context, text string, wantAll bool, want map[string]struct{}) ([]analyzer.RecognizerResult, error) {
	var all []analyzer.RecognizerResult
	offset := 0
	for offset < len(text) {
		end := offset + maxOllamaChunkBytes
		if end >= len(text) {
			end = len(text)
		} else if nl := strings.LastIndex(text[offset:end], "\n"); nl > 0 {
			end = offset + nl + 1
		}
		results, err := r.analyzeChunk(ctx, text[offset:end], offset, wantAll, want)
		if err != nil {
			return nil, err
		}
		all = append(all, results...)
		offset = end
	}
	return all, nil
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Format   string          `json:"format"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
}

func (r *OllamaNERRecognizer) analyzeChunk(ctx context.Context, chunk string, baseOffset int, wantAll bool, want map[string]struct{}) ([]analyzer.RecognizerResult, error) {
	reqBody := ollamaRequest{
		Model: r.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: ollamaNERSystemPrompt},
			{Role: "user", Content: chunk},
		},
		Format: "json",
		Stream: false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/api/chat", bytes.NewReader(body))
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
		return nil, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, b)
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, err
	}

	entities, err := parseEntities(ollamaResp.Message.Content)
	if err != nil {
		return nil, err
	}

	var results []analyzer.RecognizerResult
	for _, ent := range entities {
		if !wantAll {
			if _, ok := want[ent.Type]; !ok {
				continue
			}
		}
		for _, span := range findSpans(chunk, ent.Text) {
			results = append(results, analyzer.RecognizerResult{
				Start:          baseOffset + span[0],
				End:            baseOffset + span[1],
				Score:          0.85,
				EntityType:     ent.Type,
				RecognizerName: "NERRecognizer",
			})
		}
	}
	return results, nil
}
