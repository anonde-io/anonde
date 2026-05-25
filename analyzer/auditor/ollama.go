// Package auditor hosts implementations of analyzer.Auditor — the
// post-pipeline LLM pass that looks for PII the regex+NER stack missed.
// Ollama is the only backend; the interface lives in analyzer/ so other
// backends can be added later without changing call sites.
package auditor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anonde-io/anonde/analyzer"
)

// OllamaConfig configures the Ollama-backed auditor. Zero values are
// replaced with sensible defaults.
type OllamaConfig struct {
	// Endpoint is the Ollama HTTP base URL.
	// Default: "http://localhost:11434".
	Endpoint string

	// Model is the Ollama tag to call. Should be a capable instruction-
	// following model (7B+ recommended). Smaller models produce
	// unreliable JSON.
	// Default: "llama3.1:8b".
	Model string

	// Timeout is the wall-clock deadline for a single audit call.
	// Default: 60 seconds.
	Timeout time.Duration

	// MaxInputChars caps the document chars sent to the LLM. Very long
	// documents are truncated to fit a sensible context window. Default
	// 8000 chars (~2000 tokens). Most clinical letters fit comfortably.
	MaxInputChars int

	// Score assigned to spans the auditor emits. Lower than regex
	// matches (which carry explicit checksums or strong patterns) but
	// above the default threshold so they pass through. Default 0.70.
	Score float64
}

// Ollama implements analyzer.Auditor.
type Ollama struct {
	cfg    OllamaConfig
	client *http.Client
}

// NewOllama constructs an Ollama-backed auditor with defaults filled in
// for any zero-valued fields.
func NewOllama(cfg OllamaConfig) *Ollama {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.1:8b"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxInputChars == 0 {
		cfg.MaxInputChars = 8000
	}
	if cfg.Score == 0 {
		cfg.Score = 0.70
	}
	return &Ollama{cfg: cfg, client: &http.Client{}}
}

const auditorSystemPrompt = `You audit German clinical text for missed PII (personally identifiable information).
The text has already been processed by a redaction system. Find ANY additional PII that should be redacted but wasn't.

Valid PII categories:
- PERSON: names of people (patients, doctors, relatives)
- LOCATION: cities, states, countries
- ORGANIZATION: hospitals, clinics, practices
- DATE: any date or year
- ID: case numbers, MRNs, insurance numbers, patient identifiers
- AGE: ages (e.g. "65-jährig", "im Alter von 65")
- PHONE: phone or fax numbers
- EMAIL: email addresses
- ADDRESS: street addresses, postal codes
- PROFESSION: job titles tied to the patient or staff

NOT PII (do not return):
- Disease names, drug names, anatomical terms
- Medical procedures
- Hospital department names without identifying suffixes
- Generic dates ranges or relative time expressions ("seit Jahren")

Reply with ONLY a JSON array. Each entry must be an exact substring from the text.
Example: [{"text":"Frau Müller","type":"PERSON"},{"text":"Berlin","type":"LOCATION"}]
Return [] if no additional PII is present.`

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Format   string         `json:"format,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

type missedEntity struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// Audit calls the LLM once per document, parses its JSON reply, and
// substring-matches each returned text back into the input to produce
// byte-offset spans. Fails open: any error returns an empty slice so
// the caller's leak rate cannot increase relative to no-auditor.
func (a *Ollama) Audit(ctx context.Context, text string, known []analyzer.RecognizerResult) ([]analyzer.RecognizerResult, error) {
	if text == "" {
		return nil, nil
	}
	// Truncate very long docs to keep the prompt under the model's
	// context window. We send the head of the doc — most clinical
	// letters' PII is distributed throughout, but missed-PII tends to
	// cluster in dense narrative sections at the top.
	body := text
	if len(body) > a.cfg.MaxInputChars {
		body = body[:a.cfg.MaxInputChars]
	}

	// Build a short summary of what's already detected (so the LLM
	// focuses on what's MISSING). Cap at ~40 known spans to keep the
	// prompt size sane.
	var sb strings.Builder
	sb.WriteString("Already detected (do not repeat):\n")
	maxKnown := 40
	for i, k := range known {
		if i >= maxKnown {
			fmt.Fprintf(&sb, "… (+%d more)\n", len(known)-maxKnown)
			break
		}
		if k.Start < 0 || k.End > len(text) || k.Start >= k.End {
			continue
		}
		fmt.Fprintf(&sb, "- %q (%s)\n", text[k.Start:k.End], k.EntityType)
	}

	userMsg := fmt.Sprintf("TEXT:\n\"\"\"\n%s\n\"\"\"\n\n%s\n\nFind missed PII. Reply JSON only.",
		body, sb.String())

	req := chatRequest{
		Model:  a.cfg.Model,
		Stream: false,
		Format: "json",
		Messages: []chatMessage{
			{Role: "system", Content: auditorSystemPrompt},
			{Role: "user", Content: userMsg},
		},
		Options: map[string]any{
			"temperature": 0,
			"num_predict": 1024,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, nil // fail-open
	}

	callCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, a.cfg.Endpoint+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return nil, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(resp.Body)
		return nil, nil
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, nil
	}

	entities := parseAuditReply(cr.Message.Content)
	return a.matchSpans(text, entities, known), nil
}

// parseAuditReply tolerantly extracts the JSON array from the model's
// reply. Some models wrap output in markdown fences or precede it with
// natural-language preambles despite the JSON format hint.
func parseAuditReply(reply string) []missedEntity {
	reply = strings.TrimSpace(reply)
	// Strip markdown code fences.
	reply = strings.TrimPrefix(reply, "```json")
	reply = strings.TrimPrefix(reply, "```")
	reply = strings.TrimSuffix(reply, "```")
	reply = strings.TrimSpace(reply)

	// Find the first '[' and last ']' — outer array.
	start := strings.Index(reply, "[")
	end := strings.LastIndex(reply, "]")
	if start < 0 || end < 0 || end <= start {
		// Some models return a JSON object with an entities key.
		var wrap struct {
			Entities []missedEntity `json:"entities"`
			Items    []missedEntity `json:"items"`
			PII      []missedEntity `json:"pii"`
		}
		if json.Unmarshal([]byte(reply), &wrap) == nil {
			if len(wrap.Entities) > 0 {
				return wrap.Entities
			}
			if len(wrap.Items) > 0 {
				return wrap.Items
			}
			if len(wrap.PII) > 0 {
				return wrap.PII
			}
		}
		return nil
	}
	var ents []missedEntity
	_ = json.Unmarshal([]byte(reply[start:end+1]), &ents)
	return ents
}

// matchSpans substring-matches each LLM-returned text in the original
// document and emits RecognizerResults for matches that don't overlap
// any known span. The first occurrence is preferred; subsequent
// occurrences of the same text are also emitted (e.g., a name appearing
// in both header and body).
func (a *Ollama) matchSpans(text string, ents []missedEntity, known []analyzer.RecognizerResult) []analyzer.RecognizerResult {
	if len(ents) == 0 {
		return nil
	}
	var out []analyzer.RecognizerResult
	for _, e := range ents {
		needle := strings.TrimSpace(e.Text)
		if len(needle) < 2 {
			continue
		}
		entityType := strings.ToUpper(strings.TrimSpace(e.Type))
		if entityType == "" {
			continue
		}
		// Map free-text LLM types to anonde entity types.
		switch entityType {
		case "STREET", "POSTALCODE", "POSTAL_CODE", "ZIP":
			entityType = "STREET_ADDRESS"
		}
		// Scan for occurrences.
		idx := 0
		for idx < len(text) {
			at := strings.Index(text[idx:], needle)
			if at < 0 {
				break
			}
			start := idx + at
			end := start + len(needle)
			if !overlapsKnown(start, end, known) && !overlapsKnown(start, end, out) {
				out = append(out, analyzer.RecognizerResult{
					Start:          start,
					End:            end,
					Score:          a.cfg.Score,
					EntityType:     entityType,
					RecognizerName: "OllamaAuditor",
				})
			}
			idx = end
		}
	}
	return out
}

func overlapsKnown(start, end int, spans []analyzer.RecognizerResult) bool {
	for _, s := range spans {
		if start < s.End && end > s.Start {
			return true
		}
	}
	return false
}
