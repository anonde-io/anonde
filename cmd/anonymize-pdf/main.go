// Command anonymize-pdf is a one-shot CLI that runs the anonde
// detection + anonymisation pipeline over a PDF file and writes a
// redacted PDF to disk. The intended UX matches commercial PII
// gateways (Private AI / Limina) — `anonymize-pdf in.pdf out.pdf`
// — so self-hosters can wire anonde into the same offline batch
// workflows.
//
// Input: any PDF. Scanned PDFs (no text layer) are routed through
// the OCR fallback (pdftoppm + tesseract; multilingual by
// default — see internal/content/ocr.go).
//
// Output: a new PDF where each detected PII span is replaced
// character-for-character with the mask character (default '#').
// Use --operator=replace to emit Stripe-style <ENTITY_xxx> tokens
// instead — that path is reversible via the standard service.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/anonde-io/anonde/anonymizer/operators"
	"github.com/anonde-io/anonde/internal/content"
)

func main() {
	var (
		mode     = flag.String("mode", "visual", "redaction mode: visual (default — black boxes drawn on original page rasters, like Private AI / Limina) or text (text-only PDF with '#' substitutions)")
		operator = flag.String("operator", "mask", "text-mode operator: mask (default, prints '#') or redact (<REDACTED>)")
		maskChar = flag.String("mask-char", "#", "character used by the mask operator (text mode)")
		langs    = flag.String("langs", "", "comma- or plus-separated tesseract language list (e.g. eng+deu+ron). Empty = anonde default. Overrides ANONDE_OCR_LANGS.")
		entities = flag.String("entities", "", "comma-separated entity allow-list (e.g. PERSON,LOCATION). Empty = all known.")
		score    = flag.Float64("score-threshold", 0.3, "minimum recognizer score (matches the HTTP server default)")
		backend  = flag.String("backend", "auto", "analyzer backend: auto (default, gliner when -tags hugot build is available, else patterns), patterns, gliner, or hugot")
		dpi      = flag.Int("dpi", 200, "rasterisation DPI for visual mode")
		pad      = flag.Int("box-padding", 2, "pixels of padding around each PII word box in visual mode (helps cover OCR baseline jitter)")
		heur     = flag.Bool("visual-heuristic", true, "in visual mode, also redact ink regions not covered by confident OCR (catches signatures, stamps, logos)")
		signat   = flag.Bool("signature-model", false, "run the YOLOv8s signature-detection ONNX model (requires -tags hugot build + first-run model download)")
		signatModel = flag.String("signature-model-path", "", "override path to the signature ONNX model; default downloads on first use to ~/.cache/anonde/models/signature/")
		dumpText = flag.String("dump-text", "", "optional: write the extracted (post-OCR) raw text here for debugging")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: anonymize-pdf [flags] <input.pdf> <output.pdf>\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	in, out := flag.Arg(0), flag.Arg(1)

	if *langs != "" {
		os.Setenv("ANONDE_OCR_LANGS", strings.ReplaceAll(*langs, ",", "+"))
	}

	raw, err := os.ReadFile(in)
	if err != nil {
		log.Fatalf("read input: %v", err)
	}

	engine, backendName, err := buildAnalyzerEngine(*backend)
	if err != nil {
		log.Fatalf("backend: %v", err)
	}
	fmt.Fprintf(os.Stderr, "anonymize-pdf: backend=%s\n", backendName)
	cfg := analyzer.AnalysisConfig{
		ScoreThreshold:  *score,
		RemoveConflicts: true,
	}
	if *entities != "" {
		cfg.Entities = strings.Split(*entities, ",")
	}

	var (
		pdfBytes []byte
		findings []analyzer.RecognizerResult
	)

	switch strings.ToLower(*mode) {
	case "visual", "":
		opts := content.RedactPDFOptions{
			Engine:          engine,
			AnalysisCfg:     cfg,
			DPI:             *dpi,
			BoxPadding:      *pad,
			VisualHeuristic: *heur,
		}
		if *signat {
			detector, derr := loadSignatureDetector(*signatModel)
			if derr != nil {
				log.Fatalf("signature model: %v", derr)
			}
			opts.VisualDetector = detector
		}
		pdfBytes, findings, err = content.RedactPDFVisual(context.Background(), raw, opts)
		if err != nil {
			log.Fatalf("visual redaction: %v", err)
		}
	case "text":
		// Text-mode reuses the original ExtractAnalyzable path and
		// emits a re-rendered text PDF with '#' substitutions —
		// useful for fully-machine-readable downstream pipelines.
		b64 := base64.StdEncoding.EncodeToString(raw)
		extracted, err := content.ExtractAnalyzable(b64, content.FormatPDF)
		if err != nil {
			log.Fatalf("extract pdf text: %v", err)
		}
		if strings.TrimSpace(extracted) == "" {
			log.Fatalf("no text extracted from %s (no text layer and OCR unavailable — install pdftoppm + tesseract)", in)
		}
		if *dumpText != "" {
			if err := os.WriteFile(*dumpText, []byte(extracted), 0o600); err != nil {
				log.Fatalf("write --dump-text: %v", err)
			}
		}
		if cfg.Language == "" {
			cfg.Language = content.DetectLanguage(extracted)
		}
		findings, err = engine.Analyze(context.Background(), extracted, cfg)
		if err != nil {
			log.Fatalf("analyze: %v", err)
		}
		findings = anonymizer.MergeAdjacentSameType(findings, extracted)
		op, err := buildOperator(*operator, *maskChar)
		if err != nil {
			log.Fatalf("operator: %v", err)
		}
		anon := anonde.DefaultAnonymizerEngine()
		res, err := anon.Anonymize(extracted, findings, anonymizer.AnonymizerConfig{"*": op})
		if err != nil {
			log.Fatalf("anonymize: %v", err)
		}
		pdfBytes, err = content.RenderTextAsPDF(res.Text)
		if err != nil {
			log.Fatalf("render pdf: %v", err)
		}
	default:
		log.Fatalf("unknown --mode %q (use visual or text)", *mode)
	}

	if dir := filepath.Dir(out); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(out, pdfBytes, 0o600); err != nil {
		log.Fatalf("write output: %v", err)
	}

	// Summary to stderr so stdout stays clean for scripting.
	byType := map[string]int{}
	for _, f := range findings {
		byType[f.EntityType]++
	}
	fmt.Fprintf(os.Stderr, "anonymize-pdf: mode=%s, %d entities across %d types -> %s\n",
		*mode, len(findings), len(byType), out)
	for t, n := range byType {
		fmt.Fprintf(os.Stderr, "  %s: %d\n", t, n)
	}
}

func buildOperator(name, maskChar string) (anonymizer.Operator, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mask", "":
		ch := "#"
		if maskChar != "" {
			ch = maskChar
		}
		return &operators.Mask{MaskingChar: ch}, nil
	case "redact":
		return &operators.Redact{}, nil
	default:
		return nil, fmt.Errorf("unsupported operator %q (use mask or redact)", name)
	}
}
