package content

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// OCR configuration knobs (env-driven so self-hosters can tune without
// rebuilding). All have safe defaults; the only hard requirement to
// enable OCR is having `pdftoppm` and `tesseract` on PATH.
const (
	envOCREnabled = "ANONDE_OCR_ENABLED"
	envOCRLangs   = "ANONDE_OCR_LANGS"
	envOCRDPI     = "ANONDE_OCR_DPI"
	// Threshold under which the text-layer extraction is considered
	// "empty" and OCR fallback kicks in. PDF text layers can hold
	// stray whitespace or single-line metadata even on pure scans,
	// so a small floor avoids false negatives.
	envOCRTextFloor = "ANONDE_OCR_TEXT_FLOOR"

	// Default language set covers anonde's primary positioning
	// (global, with strong DE / Romance support). Tesseract loads
	// each model once per process; combining the common European
	// scripts costs ~200 MB RAM peak but reads multilingual docs
	// (German legal, Romanian government forms, French contracts)
	// without per-doc tuning. Override via ANONDE_OCR_LANGS; e.g.
	// "eng" alone is faster when the corpus is known English-only.
	defaultOCRLangs     = "eng+deu+fra+spa+ita+ron"
	defaultOCRDPI       = "300"
	defaultOCRTextFloor = 64
)

// ocrAvailable reports whether the required external binaries are
// reachable on PATH. Callers should treat false as "skip OCR
// silently" rather than as an error; OCR is an opt-in fallback.
func ocrAvailable() bool {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envOCREnabled))); v == "false" || v == "0" || v == "off" {
		return false
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return false
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		return false
	}
	return true
}

// ocrTextFloor returns the byte count under which the text-layer
// extraction is treated as empty and OCR is attempted.
func ocrTextFloor() int {
	v := strings.TrimSpace(os.Getenv(envOCRTextFloor))
	if v == "" {
		return defaultOCRTextFloor
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return defaultOCRTextFloor
	}
	return n
}

// ocrLangs returns the Tesseract language string (e.g. "eng+deu+ron").
// Honors ANONDE_OCR_LANGS; defaults to "eng+deu" so the patterns-only
// image works out of the box for the primary corpora.
func ocrLangs() string {
	if v := strings.TrimSpace(os.Getenv(envOCRLangs)); v != "" {
		return v
	}
	return defaultOCRLangs
}

// ocrDPI returns the rasterization DPI as a string (passed directly to
// pdftoppm -r). 300 is a good general default for OCR accuracy vs
// runtime.
func ocrDPI() string {
	if v := strings.TrimSpace(os.Getenv(envOCRDPI)); v != "" {
		return v
	}
	return defaultOCRDPI
}

// OCRPDFBytes rasterises a PDF with pdftoppm then OCRs each page with
// tesseract, returning concatenated text separated by form-feed
// characters (matching how pdftotext separates pages, which is what
// the analyzer downstream is used to).
//
// Returns ("", nil), not an error, when OCR is unavailable, so
// callers can wire it as a soft fallback after the text-layer path.
func OCRPDFBytes(raw []byte) (string, error) {
	if !ocrAvailable() {
		return "", nil
	}
	tmpDir, err := os.MkdirTemp("", "anonde-ocr-")
	if err != nil {
		return "", fmt.Errorf("ocr: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "in.pdf")
	if err := os.WriteFile(pdfPath, raw, 0o600); err != nil {
		return "", fmt.Errorf("ocr: write temp pdf: %w", err)
	}
	pagePrefix := filepath.Join(tmpDir, "page")
	cmd := exec.Command("pdftoppm", "-r", ocrDPI(), "-png", pdfPath, pagePrefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ocr: pdftoppm failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	matches, err := filepath.Glob(pagePrefix + "-*.png")
	if err != nil {
		return "", fmt.Errorf("ocr: glob rasterized pages: %w", err)
	}
	if len(matches) == 0 {
		return "", nil
	}
	sort.Strings(matches)

	langs := ocrLangs()
	var out strings.Builder
	for i, img := range matches {
		txt, err := tesseractText(img, langs)
		if err != nil {
			return "", err
		}
		if i > 0 {
			out.WriteByte('\f')
		}
		out.WriteString(strings.TrimRight(txt, "\n"))
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String()), nil
}

func tesseractText(image, langs string) (string, error) {
	// `tesseract <img> stdout -l <langs>` emits plain text on stdout.
	// PSM 3 (fully automatic page segmentation) is tesseract's default
	// and works best on real-world scans; it preserves table-row
	// reading order on multi-column government forms (Romanian
	// garnishment notices were the motivating case: PSM 6 was reading
	// only every other table row, losing per-row IBANs and amounts).
	// PSM 6 ("single block") is faster but loses recall on anything
	// with whitespace gaps between columns.
	cmd := exec.Command("tesseract", image, "stdout", "-l", langs, "--psm", "3")
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("ocr: tesseract failed on %s: %w: %s", filepath.Base(image), err, stderr)
	}
	return string(out), nil
}
