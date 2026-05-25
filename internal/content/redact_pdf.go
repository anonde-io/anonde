package content

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/anonymizer"
	"github.com/go-pdf/fpdf"

	// JPEG decoder registration is needed so image.Decode handles pages
	// produced via `pdftoppm -jpeg` if a caller swaps the encoder; the
	// default path stays on PNG.
	_ "image/jpeg"
)

// OCRWord captures a single recognized word — its text plus the
// page-local pixel bounding box and tesseract confidence (0-100).
type OCRWord struct {
	Page         int
	Text         string
	Left, Top    int
	Width, Heigh int
	Conf         float64
}

// CharSpan attaches a character-offset range in the concatenated OCR
// text to an OCRWord, so analyzer findings (which are char-indexed)
// can be mapped back to page coordinates for drawing.
type CharSpan struct {
	Word       OCRWord
	StartChar  int
	EndChar    int // exclusive
}

// PageRaster holds the path to a rasterized PDF page and its OCR words.
type PageRaster struct {
	PNGPath string
	Words   []OCRWord
	// PixelWidth / PixelHeight describe the rasterised image
	// dimensions; needed when scaling word bboxes to PDF point
	// coordinates for the output.
	PixelWidth, PixelHeight int
}

// RedactPDFOptions controls visual-redaction behavior. The zero value
// is a sensible default — patterns-only analyzer, black-fill boxes,
// 200 DPI rasterisation.
type RedactPDFOptions struct {
	// Engine performs the PII detection on the OCR'd text. Required.
	Engine *analyzer.AnalyzerEngine
	// AnalysisCfg is passed through to Engine.Analyze. Language is
	// auto-detected when empty.
	AnalysisCfg analyzer.AnalysisConfig
	// FillColor is the box fill — defaults to opaque black.
	FillColor color.RGBA
	// BoxPadding adds N pixels in each direction around the bbox
	// before filling. Helps cover OCR baseline jitter / drop-shadow
	// artefacts. Default 2.
	BoxPadding int
	// DPI for rasterisation. 200 keeps file sizes reasonable while
	// remaining legible; bump to 300 for archival-quality output.
	DPI int
	// VisualHeuristic, when true, runs the dependency-free pixel-density
	// heuristic to catch handwritten signatures, ink stamps, logos, and
	// other visual PII the OCR pipeline doesn't surface as text.
	VisualHeuristic bool
	// VisualHeuristicOpts overrides the heuristic's defaults; zero value
	// is fine for most documents.
	VisualHeuristicOpts VisualHeuristicOptions
	// VisualDetector, when non-nil, runs in addition to the heuristic
	// and produces bounding boxes from a real vision model
	// (e.g. tech4humans/yolov8s-signature-detector via
	// yalue/onnxruntime_go). Built only with -tags hugot.
	VisualDetector VisualDetector
}

// VisualDetector is the contract for vision-model object detectors that
// participate in PDF redaction. Implementations return a list of
// axis-aligned rectangles in the page-image's pixel coordinate system;
// the redactor unions them with the heuristic boxes and the GLiNER
// text boxes before drawing fills.
type VisualDetector interface {
	Detect(img image.Image) ([]image.Rectangle, error)
}

// RedactPDFVisual is the "match Limina" path: rasterise every page,
// OCR with word-level bounding boxes, run anonde's analyzer on the
// concatenated text, then paint filled rectangles over each PII word
// on its page raster. The output is a new PDF where every page is the
// (now-redacted) raster of the corresponding input page — original
// visual structure preserved, PII pixels obliterated.
//
// Returns the new PDF bytes plus the structured findings (so callers
// can log / report what was redacted). Findings carry char offsets in
// the OCR-concatenated text, not the original PDF.
func RedactPDFVisual(ctx context.Context, raw []byte, opts RedactPDFOptions) ([]byte, []analyzer.RecognizerResult, error) {
	if opts.Engine == nil {
		return nil, nil, fmt.Errorf("RedactPDFVisual: AnalyzerEngine required")
	}
	if !ocrAvailable() {
		return nil, nil, fmt.Errorf("RedactPDFVisual: pdftoppm + tesseract must be installed")
	}
	if opts.DPI <= 0 {
		opts.DPI = 200
	}
	if opts.BoxPadding == 0 {
		opts.BoxPadding = 2
	}
	if opts.FillColor == (color.RGBA{}) {
		// Off-white / very light grey — matches the visual style of
		// commercial PII gateways (Private AI / Limina). Slightly
		// darker than pure white so a reviewer can still see WHERE
		// PII was on a scanned-paper background, without the box
		// screaming "REDACTED" at them. RGB(230,230,230) is the
		// neutral grey commercial redactors converge on.
		opts.FillColor = color.RGBA{230, 230, 230, 255}
	}

	tmpDir, err := os.MkdirTemp("", "anonde-redact-")
	if err != nil {
		return nil, nil, fmt.Errorf("redact: temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "in.pdf")
	if err := os.WriteFile(pdfPath, raw, 0o600); err != nil {
		return nil, nil, fmt.Errorf("redact: write pdf: %w", err)
	}

	pages, err := rasterizePDF(pdfPath, tmpDir, opts.DPI)
	if err != nil {
		return nil, nil, err
	}
	if len(pages) == 0 {
		return nil, nil, fmt.Errorf("redact: no pages rasterized")
	}

	var (
		fullText strings.Builder
		spans    []CharSpan
	)
	for i, p := range pages {
		words, err := ocrPageTSV(p.PNGPath, ocrLangs())
		if err != nil {
			return nil, nil, err
		}
		// Concatenate words for this page with single spaces.
		// Pages separated by \f. Track char ranges per word.
		for _, w := range words {
			w.Page = i
			start := fullText.Len()
			fullText.WriteString(w.Text)
			end := fullText.Len()
			fullText.WriteByte(' ')
			pages[i].Words = append(pages[i].Words, w)
			spans = append(spans, CharSpan{Word: w, StartChar: start, EndChar: end})
		}
		if i < len(pages)-1 {
			fullText.WriteByte('\f')
		}
	}

	text := fullText.String()
	cfg := opts.AnalysisCfg
	cfg.RemoveConflicts = true
	if cfg.Language == "" {
		cfg.Language = DetectLanguage(text)
	}
	findings, err := opts.Engine.Analyze(ctx, text, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("redact: analyze: %w", err)
	}
	findings = anonymizer.MergeAdjacentSameType(findings, text)

	debugDump := os.Getenv("ANONDE_FINDINGS_DEBUG") != ""
	// rectsPerPage holds the redaction rectangles per page. We
	// compute each rect from the OCR word bbox proportionally to the
	// character-range overlap between the finding and the word —
	// this matters for tokens where tesseract glued PII to non-PII
	// (e.g. "250,00|RO57TREZ..." emitted as one word): we want to
	// black out the IBAN slice without eating the "250,00" amount.
	rectsPerPage := map[int][]image.Rectangle{}
	for _, f := range findings {
		if debugDump {
			a, b := f.Start, f.End
			if a < 0 {
				a = 0
			}
			if b > len(text) {
				b = len(text)
			}
			fmt.Fprintf(os.Stderr, "finding: type=%s score=%.2f start=%d end=%d text=%q\n",
				f.EntityType, f.Score, f.Start, f.End, text[a:b])
		}
		for _, s := range spans {
			if s.EndChar <= f.Start || s.StartChar >= f.End {
				continue
			}
			wLen := s.EndChar - s.StartChar
			if wLen <= 0 {
				continue
			}
			// Character offsets of the finding *within* the word
			// (clamped to the word's range).
			relStart := f.Start - s.StartChar
			if relStart < 0 {
				relStart = 0
			}
			relEnd := f.End - s.StartChar
			if relEnd > wLen {
				relEnd = wLen
			}
			// Proportionally slice the word's pixel bbox. If the
			// finding covers the WHOLE word, this collapses to the
			// full bbox (the common case). For partial matches we
			// bleed one char-width into the non-PII side so OCR /
			// monospace-vs-proportional width drift can't leave the
			// first/last PII glyph uncovered (off-by-one is a
			// privacy leak; over-covering a prefix character is
			// not).
			w := s.Word
			pxStart := w.Left + (w.Width*relStart)/wLen
			pxEnd := w.Left + (w.Width*relEnd)/wLen
			if relStart > 0 {
				bleed := w.Width / wLen // ≈ 1 char width
				if bleed < 4 {
					bleed = 4
				}
				pxStart -= bleed
				if pxStart < w.Left {
					pxStart = w.Left
				}
			}
			if relEnd < wLen {
				bleed := w.Width / wLen
				if bleed < 4 {
					bleed = 4
				}
				pxEnd += bleed
				if pxEnd > w.Left+w.Width {
					pxEnd = w.Left + w.Width
				}
			}
			rect := image.Rect(pxStart, w.Top, pxEnd, w.Top+w.Heigh)
			rectsPerPage[w.Page] = append(rectsPerPage[w.Page], rect)
		}
	}

	// Render each page raster with redaction rectangles drawn,
	// re-encode as PNG, and embed into a fresh PDF where each PDF
	// page mirrors the original page size in points.
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(0, 0, 0)
	for i, p := range pages {
		img, err := loadPNG(p.PNGPath)
		if err != nil {
			return nil, nil, fmt.Errorf("redact: load page %d: %w", i+1, err)
		}
		canvas := image.NewRGBA(img.Bounds())
		draw.Draw(canvas, img.Bounds(), img, image.Point{}, draw.Src)
		filler := &image.Uniform{C: opts.FillColor}
		for _, rect := range rectsPerPage[i] {
			padded := image.Rect(
				rect.Min.X-opts.BoxPadding,
				rect.Min.Y-opts.BoxPadding,
				rect.Max.X+opts.BoxPadding,
				rect.Max.Y+opts.BoxPadding,
			).Intersect(canvas.Bounds())
			draw.Draw(canvas, padded, filler, image.Point{}, draw.Src)
		}
		// Visual heuristic — overlay extra boxes over ink regions the
		// OCR didn't claim as confident text (signatures, stamps,
		// logos). Runs on the ORIGINAL page raster, not the
		// already-redacted canvas, so freshly-blacked text boxes don't
		// register as new "ink."
		if opts.VisualHeuristic {
			hOpts := opts.VisualHeuristicOpts
			if hOpts.TileSize == 0 {
				hOpts = defaultVisualOpts()
			}
			for _, rect := range detectVisualPIIHeuristic(img, p.Words, hOpts) {
				draw.Draw(canvas, rect, filler, image.Point{}, draw.Src)
			}
		}
		if opts.VisualDetector != nil {
			// Skip the ~500 ms vision inference on pages that are
			// clearly text-only (no non-text ink anywhere). Cheap
			// O(tiles × words) check; pays for itself on the first
			// text-only page. Skipped pages still get the text-PII
			// redactions above and any heuristic boxes.
			hOpts := opts.VisualHeuristicOpts
			if hOpts.TileSize == 0 {
				hOpts = defaultVisualOpts()
			}
			if isLikelyTextOnlyPage(img, p.Words, hOpts) {
				if os.Getenv("ANONDE_VISION_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "page %d: text-only, skipping vision\n", i+1)
				}
			} else {
				boxes, derr := opts.VisualDetector.Detect(img)
				if derr != nil {
					return nil, nil, fmt.Errorf("redact: visual detector on page %d: %w", i+1, derr)
				}
				for _, rect := range boxes {
					draw.Draw(canvas, rect.Intersect(canvas.Bounds()), filler, image.Point{}, draw.Src)
				}
			}
		}
		pages[i].PixelWidth = canvas.Bounds().Dx()
		pages[i].PixelHeight = canvas.Bounds().Dy()

		var buf bytes.Buffer
		if err := png.Encode(&buf, canvas); err != nil {
			return nil, nil, fmt.Errorf("redact: encode page %d: %w", i+1, err)
		}
		imgName := fmt.Sprintf("page-%d", i+1)
		opt := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
		pdf.RegisterImageOptionsReader(imgName, opt, &buf)

		// Convert pixels to points: 1pt = 1/72in, image is at opts.DPI
		// pixels per inch.
		ptW := float64(pages[i].PixelWidth) * 72.0 / float64(opts.DPI)
		ptH := float64(pages[i].PixelHeight) * 72.0 / float64(opts.DPI)
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: ptW, Ht: ptH})
		pdf.ImageOptions(imgName, 0, 0, ptW, ptH, false, opt, 0, "")
	}

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, nil, fmt.Errorf("redact: pdf output: %w", err)
	}
	return out.Bytes(), findings, nil
}

func rasterizePDF(pdfPath, outDir string, dpi int) ([]PageRaster, error) {
	prefix := filepath.Join(outDir, "page")
	cmd := exec.Command("pdftoppm", "-r", strconv.Itoa(dpi), "-png", pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("rasterize: pdftoppm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	matches, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return nil, fmt.Errorf("rasterize: glob: %w", err)
	}
	sort.Strings(matches)
	pages := make([]PageRaster, len(matches))
	for i, m := range matches {
		pages[i] = PageRaster{PNGPath: m}
	}
	return pages, nil
}

// ocrPageTSV invokes tesseract with TSV output and returns the
// word-level rows (TSV level 5). Confidence below 0 (header row /
// block / line rows that have no text) is skipped.
func ocrPageTSV(image, langs string) ([]OCRWord, error) {
	cmd := exec.Command("tesseract", image, "stdout", "-l", langs, "--psm", "3", "tsv")
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("ocr tsv: %w: %s", err, stderr)
	}
	r := csv.NewReader(bytes.NewReader(out))
	r.Comma = '\t'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("ocr tsv parse: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil
	}
	var words []OCRWord
	for _, row := range rows[1:] {
		if len(row) < 12 {
			continue
		}
		level := row[0]
		if level != "5" {
			continue
		}
		text := strings.TrimSpace(row[11])
		if text == "" {
			continue
		}
		l, _ := strconv.Atoi(row[6])
		t, _ := strconv.Atoi(row[7])
		w, _ := strconv.Atoi(row[8])
		h, _ := strconv.Atoi(row[9])
		conf, _ := strconv.ParseFloat(row[10], 64)
		words = append(words, OCRWord{Text: text, Left: l, Top: t, Width: w, Heigh: h, Conf: conf})
	}
	return words, nil
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}
