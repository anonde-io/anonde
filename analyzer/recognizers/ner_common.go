package recognizers

import (
	"encoding/json"
	"strings"
)

// llmEntity is the JSON shape returned by the NER model response.
type llmEntity struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// parseEntities extracts the JSON entity list from a model response.
// It tolerates markdown code fences and leading/trailing prose.
func parseEntities(raw string) ([]llmEntity, error) {
	if i := strings.Index(raw, "```"); i >= 0 {
		raw = raw[i+3:]
		if j := strings.Index(raw, "```"); j >= 0 {
			raw = raw[:j]
		}
		raw = strings.TrimPrefix(raw, "json")
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 {
		raw = raw[:i+1]
	}

	var payload struct {
		Entities []llmEntity `json:"entities"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload.Entities, nil
}

// findSpans returns all [start, end) byte positions of needle in text.
func findSpans(text, needle string) [][2]int {
	if needle == "" {
		return nil
	}
	var spans [][2]int
	for i := 0; i < len(text); {
		idx := strings.Index(text[i:], needle)
		if idx < 0 {
			break
		}
		start := i + idx
		spans = append(spans, [2]int{start, start + len(needle)})
		i = start + len(needle)
	}
	return spans
}
