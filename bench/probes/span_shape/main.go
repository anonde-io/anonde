// bench/probes/span_shape is a model-free precision probe for the
// structural-shape span filter (analyzer/recognizers/span_shape_filter.go).
// It tests the post-filter DECISION, not the model, so it needs no CGO /
// libonnxruntime / model download.
//
// The fixture pairs structural FPs (model slugs, UUIDs, locales, versions,
// hex/base64 blobs, SCREAMING_SNAKE, dotted paths — gold: keep) with real
// PII (names, orgs, places, NRP, professions, ages across EN/DE/ES/FR/asian
// scripts — gold: redact). "before" = filter OFF; "after" = StrictSpanFilter.
// The probe exits non-zero if the filter drops any real-PII surface (a
// leak), so it can gate CI as a canary.
//
// Run:  go run ./bench/probes/span_shape          # text report
//
//	go run ./bench/probes/span_shape -json     # machine-readable
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/anonde-io/anonde/analyzer/recognizers"
)

// sample is one surface the GLiNER decoder is assumed to PROPOSE as the
// given fuzzy entity type. redact=true means it is real PII that must
// survive the filter; redact=false means it is a structural FP that the
// filter should drop.
type sample struct {
	surface string
	typ     string
	redact  bool // gold: should this be redacted?
	klass   string
}

func fixture() []sample {
	var out []sample
	add := func(klass, typ string, redact bool, surfaces ...string) {
		for _, s := range surfaces {
			out = append(out, sample{surface: s, typ: typ, redact: redact, klass: klass})
		}
	}

	// ---- STRUCTURAL FALSE POSITIVES (gold: do NOT redact) -------------
	add("model-slug", "ORGANIZATION", false,
		"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo", "text-davinci-003",
		"llama-2", "llama-3", "claude-3", "claude-3.5", "gemini-pro",
		"mistral-7b", "qwen-2", "command-r")
	add("model-name", "PERSON", false,
		"claude", "sonnet", "opus", "haiku", "gemini", "davinci", "bard")
	add("uuid", "PERSON", false,
		"550e8400-e29b-41d4-a716-446655440000",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"{f47ac10b-58cc-4372-a567-0e02b2c3d479}")
	add("uuid", "ORGANIZATION", false,
		"00000000-0000-0000-0000-000000000000")
	add("locale", "LOCATION", false,
		"en-US", "de_DE", "fr-FR", "pt-BR", "zh-Hans-CN", "es_ES")
	add("version", "ORGANIZATION", false,
		"v1.2.3", "1.81.1", "2.0.0-rc1", "v3", "10.15.7", "v0.1.0-beta")
	add("hex-blob", "PERSON", false,
		"deadbeefcafebabe1234", "a1b2c3d4e5f6a7b8c9d0e1f2")
	add("base64-blob", "ORGANIZATION", false,
		"dGhpc2lzYXNlY3JldHRva2VuMTIz", "eyJhbGciOiJIUzI1NiJ9abcd1234")
	add("screaming-snake", "ORGANIZATION", false,
		"HTTP_X_FORWARDED_FOR", "API_KEY", "MAX_RETRIES", "ENABLE_TELEMETRY")
	add("dotted-path", "ORGANIZATION", false,
		"com.example.service", "app.config.timeout.ms", "io.grpc.Status")
	add("digit-punct", "LOCATION", false,
		"12:34:56", "1,234.56", "----", "0.0.0.0")
	add("tech-term", "PERSON", false,
		"json", "yaml", "bearer", "oauth", "null", "undefined")

	// ---- REAL PII (gold: MUST redact) ---------------------------------
	add("person", "PERSON", true,
		"Maria Lopez", "John Doe", "Dr. Schmidt", "Jean-Pierre Dubois",
		"Anna-Lena Weber", "Müller", "O'Brien", "李伟", "García",
		"Federica Bianchi", "Søren Kierkegaard", "Ngô Bảo Châu")
	add("org", "ORGANIZATION", true,
		"Acme Corp", "Deutsche Bank AG", "Universitätsklinikum Heidelberg",
		"Côte d'Or", "Banco Santander", "Société Générale", "Siemens",
		"St. Mary's Hospital")
	add("location", "LOCATION", true,
		"New York", "München", "São Paulo", "Baden-Württemberg",
		"Côte d'Azur", "Frankfurt am Main", "Stratford-upon-Avon")
	add("nrp", "NRP", true,
		"German", "Catholic", "Democratic Party", "Han Chinese", "Latino")
	add("profession", "PROFESSION", true,
		"software engineer", "Oberarzt", "data scientist", "Rechtsanwalt")
	add("age", "AGE", true,
		"42", "42 years", "thirty", "67-year-old")

	return out
}

type metrics struct {
	tp, fp, fn, tn int
}

func (m metrics) precision() float64 {
	if m.tp+m.fp == 0 {
		return 1.0
	}
	return float64(m.tp) / float64(m.tp+m.fp)
}
func (m metrics) recall() float64 {
	if m.tp+m.fn == 0 {
		return 1.0
	}
	return float64(m.tp) / float64(m.tp+m.fn)
}

// fpRate over the negatives (structural surfaces): fp / (fp + tn).
func (m metrics) fpRate() float64 {
	if m.fp+m.tn == 0 {
		return 0.0
	}
	return float64(m.fp) / float64(m.fp+m.tn)
}

// score runs the fixture under a given filter (nil = OFF / before) and
// returns metrics overall, by entity type, and by structural class.
func score(samples []sample, f *recognizers.SpanFilterConfig) (overall metrics, byType, byClass map[string]*metrics) {
	byType = map[string]*metrics{}
	byClass = map[string]*metrics{}
	get := func(m map[string]*metrics, k string) *metrics {
		if m[k] == nil {
			m[k] = &metrics{}
		}
		return m[k]
	}
	for _, s := range samples {
		// kept = span survives to redaction (always, with filter OFF).
		kept := true
		if f != nil && f.Enabled {
			kept = !f.Reject(s.typ, s.surface)
		}
		t := get(byType, s.typ)
		c := get(byClass, s.klass)
		switch {
		case s.redact && kept: // true positive
			overall.tp++
			t.tp++
			c.tp++
		case s.redact && !kept: // false negative (LEAK)
			overall.fn++
			t.fn++
			c.fn++
		case !s.redact && kept: // false positive
			overall.fp++
			t.fp++
			c.fp++
		default: // !redact && !kept: true negative (correctly dropped)
			overall.tn++
			t.tn++
			c.tn++
		}
	}
	return overall, byType, byClass
}

func main() {
	jsonOut := flag.Bool("json", false, "emit machine-readable JSON")
	stoplist := flag.String("stoplist", "", "comma-separated extra stoplist terms")
	flag.Parse()

	samples := fixture()
	var extra []string
	for _, t := range splitComma(*stoplist) {
		extra = append(extra, t)
	}
	on := recognizers.StrictSpanFilter(extra...)

	beforeAll, beforeType, beforeClass := score(samples, nil)
	afterAll, afterType, afterClass := score(samples, &on)

	if *jsonOut {
		emitJSON(beforeAll, afterAll, beforeType, afterType, beforeClass, afterClass)
	} else {
		emitText(samples, beforeAll, afterAll, beforeType, afterType, beforeClass, afterClass)
	}

	// CANARY: any real-PII surface dropped by the filter is a LEAK; fail.
	if afterAll.fn > beforeAll.fn {
		fmt.Fprintf(os.Stderr, "\nFAIL: STRICT filter introduced %d new false negatives (leaks)\n", afterAll.fn-beforeAll.fn)
		os.Exit(1)
	}
}

func emitText(samples []sample, bA, aA metrics, bT, aT, bC, aC map[string]*metrics) {
	fmt.Println("=== span-shape precision probe (model-free) ===")
	fmt.Printf("fixture: %d surfaces (%d real PII, %d structural FP)\n\n",
		len(samples), countRedact(samples, true), countRedact(samples, false))

	fmt.Println("OVERALL              before        after")
	fmt.Printf("  precision         %7.3f      %7.3f   (%+.3f)\n", bA.precision(), aA.precision(), aA.precision()-bA.precision())
	fmt.Printf("  recall            %7.3f      %7.3f   (%+.3f)\n", bA.recall(), aA.recall(), aA.recall()-bA.recall())
	fmt.Printf("  FP (structural)   %7d      %7d\n", bA.fp, aA.fp)
	fmt.Printf("  FN (leaks)        %7d      %7d\n\n", bA.fn, aA.fn)

	fmt.Println("BY ENTITY TYPE       precision(before→after)   recall(before→after)   FP(before→after)")
	for _, k := range sortedKeys(aT) {
		b, a := zero(bT[k]), aT[k]
		fmt.Printf("  %-16s %6.3f → %-6.3f          %6.3f → %-6.3f        %d → %d\n",
			k, b.precision(), a.precision(), b.recall(), a.recall(), b.fp, a.fp)
	}

	fmt.Println("\nSTRUCTURAL-FP CLASS  FP-rate(before→after)   kept→dropped")
	for _, k := range sortedKeys(aC) {
		a := aC[k]
		// only show structural classes (those with negatives).
		if a.fp+a.tn == 0 {
			continue
		}
		b := zero(bC[k])
		fmt.Printf("  %-16s %6.3f → %-6.3f        %d kept → %d dropped (of %d)\n",
			k, b.fpRate(), a.fpRate(), a.fp, a.tn, a.fp+a.tn)
	}

	fmt.Printf("\nstructural-FP-class FP-rate: %.3f → %.3f\n",
		structuralFPRate(bC), structuralFPRate(aC))
}

func emitJSON(bA, aA metrics, bT, aT, bC, aC map[string]*metrics) {
	type cell struct {
		Precision float64 `json:"precision"`
		Recall    float64 `json:"recall"`
		FP        int     `json:"fp"`
		FN        int     `json:"fn"`
		FPRate    float64 `json:"fp_rate"`
	}
	mk := func(m metrics) cell {
		return cell{m.precision(), m.recall(), m.fp, m.fn, m.fpRate()}
	}
	doc := map[string]any{
		"overall": map[string]cell{"before": mk(bA), "after": mk(aA)},
		"structural_fp_rate": map[string]float64{
			"before": structuralFPRate(bC), "after": structuralFPRate(aC),
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

// structuralFPRate aggregates fp / (fp+tn) across the structural classes.
func structuralFPRate(byClass map[string]*metrics) float64 {
	var fp, tn int
	for _, m := range byClass {
		if m.fp+m.tn == 0 {
			continue // a real-PII class, skip
		}
		fp += m.fp
		tn += m.tn
	}
	if fp+tn == 0 {
		return 0
	}
	return float64(fp) / float64(fp+tn)
}

func countRedact(s []sample, want bool) int {
	n := 0
	for _, x := range s {
		if x.redact == want {
			n++
		}
	}
	return n
}

func sortedKeys(m map[string]*metrics) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func zero(m *metrics) metrics {
	if m == nil {
		return metrics{}
	}
	return *m
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
