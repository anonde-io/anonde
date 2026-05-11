package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/moogacs/anonde"
	"github.com/moogacs/anonde/analyzer"
	"github.com/moogacs/anonde/anonymizer"
	"github.com/moogacs/anonde/anonymizer/operators"
)

func main() {
	listen := flag.String("listen", ":8080", "address to listen on")
	upstream := flag.String("upstream", "", "upstream URL to proxy to (required)")
	ner := flag.String("ner", "patterns", "NER backend: patterns | hugot | ollama")
	ollamaURL := flag.String("ollama-url", "", "Ollama endpoint (for -ner=ollama, default http://localhost:11434)")
	ollamaModel := flag.String("ollama-model", "", "Ollama model (for -ner=ollama, default phi3:mini)")
	scrubReq := flag.Bool("scrub-request", true, "scrub PII from request body")
	scrubResp := flag.Bool("scrub-response", false, "scrub PII from response body")
	threshold := flag.Float64("score-threshold", 0.3, "minimum confidence score (0–1)")
	disableNER := flag.Bool("disable-ner", false, "pattern-only mode — skip NER, maximum throughput")
	operatorMode := flag.String("operator", "replace", "anonymization operator: replace | synthesize")
	synConsistent := flag.Bool("synthesize-consistent", false, "synthesize: same input always maps to same fake value")
	synDocScoped := flag.Bool("synthesize-doc-scoped", false, "synthesize: consistent within each request; reset between requests (requires -synthesize-consistent)")
	flag.Parse()

	if *upstream == "" {
		log.Fatal("-upstream is required")
	}
	upstreamURL, err := url.Parse(*upstream)
	if err != nil {
		log.Fatalf("invalid upstream URL: %v", err)
	}

	var analyzerEngine *analyzer.AnalyzerEngine
	switch *ner {
	case "ollama":
		analyzerEngine = anonde.DefaultAnalyzerEngineWithOllama(*ollamaURL, *ollamaModel)
	case "hugot":
		analyzerEngine = anonde.DefaultAnalyzerEngineWithHugot("", "", true)
	default:
		analyzerEngine = anonde.DefaultAnalyzerEngine()
	}

	anonymizerEngine := anonde.DefaultAnonymizerEngine()
	analysisCfg := analyzer.AnalysisConfig{
		Language:        "en",
		ScoreThreshold:  *threshold,
		RemoveConflicts: true,
		DisableNER:      *disableNER,
	}

	// Build the operator used for every entity ("*" catch-all).
	// For synthesize in doc-scoped mode we hold a pointer so we can Reset()
	// between requests; for all other modes the operator is stateless.
	var syn *operators.Synthesize
	buildAnonCfg := func() anonymizer.AnonymizerConfig {
		switch *operatorMode {
		case "synthesize":
			return anonymizer.AnonymizerConfig{"*": syn}
		default:
			return anonymizer.AnonymizerConfig{"*": &operators.Replace{}}
		}
	}

	if *operatorMode == "synthesize" {
		syn = &operators.Synthesize{
			Consistent:     *synConsistent,
			DocumentScoped: *synDocScoped,
		}
	}

	anonCfg := buildAnonCfg()

	scrub := func(text string) (string, error) {
		results, err := analyzerEngine.Analyze(context.Background(), text, analysisCfg)
		if err != nil || len(results) == 0 {
			return text, err
		}
		out, err := anonymizerEngine.Anonymize(text, results, anonCfg)
		if err != nil {
			return text, err
		}
		return out.Text, nil
	}

	scrubBody := func(body []byte, contentType string) []byte {
		ct := strings.ToLower(contentType)
		var out []byte
		var err error
		switch {
		case strings.Contains(ct, "application/json"):
			out, err = scrubJSON(body, scrub)
		case strings.Contains(ct, "text/"):
			var s string
			s, err = scrub(string(body))
			out = []byte(s)
		default:
			return body
		}
		if err != nil {
			log.Printf("scrub error: %v", err)
			return body
		}
		return out
	}

	rp := httputil.NewSingleHostReverseProxy(upstreamURL)

	base := rp.Director
	rp.Director = func(req *http.Request) {
		base(req)
		// Reset doc-scoped synthesizer at the start of each request so that
		// the same entity text gets a fresh alias per request rather than
		// leaking aliases across unrelated requests.
		if syn != nil && syn.DocumentScoped {
			syn.Reset()
		}
		if !*scrubReq || req.Body == nil {
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return
		}
		clean := scrubBody(body, req.Header.Get("Content-Type"))
		req.Body = io.NopCloser(bytes.NewReader(clean))
		req.ContentLength = int64(len(clean))
	}

	if *scrubResp {
		rp.ModifyResponse = func(resp *http.Response) error {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				resp.Body = io.NopCloser(bytes.NewReader(body))
				return err
			}
			clean := scrubBody(body, resp.Header.Get("Content-Type"))
			resp.Body = io.NopCloser(bytes.NewReader(clean))
			resp.ContentLength = int64(len(clean))
			return nil
		}
	}

	log.Printf("anonde proxy  %s → %s  (ner=%s  operator=%s  scrub-req=%v  scrub-resp=%v)",
		*listen, *upstream, *ner, *operatorMode, *scrubReq, *scrubResp)
	log.Fatal(http.ListenAndServe(*listen, rp))
}

// scrubJSON walks a JSON document and anonymizes all string leaf values.
func scrubJSON(data []byte, scrub func(string) (string, error)) ([]byte, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return data, nil // not valid JSON — pass through
	}
	walked, err := walkJSON(v, scrub)
	if err != nil {
		return data, err
	}
	return json.Marshal(walked)
}

func walkJSON(v any, scrub func(string) (string, error)) (any, error) {
	switch val := v.(type) {
	case string:
		return scrub(val)
	case map[string]any:
		for k, child := range val {
			s, err := walkJSON(child, scrub)
			if err != nil {
				return nil, err
			}
			val[k] = s
		}
		return val, nil
	case []any:
		for i, child := range val {
			s, err := walkJSON(child, scrub)
			if err != nil {
				return nil, err
			}
			val[i] = s
		}
		return val, nil
	default:
		return v, nil
	}
}
