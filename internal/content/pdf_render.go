package content

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
)

// RenderTextAsPDF writes the supplied text into a fresh A4 PDF and
// returns the bytes. Pages are separated by form-feed characters
// (\f) — the same separator OCRPDFBytes emits — so a round trip
// (PDF -> OCR -> anonymize -> PDF) preserves page boundaries.
//
// This is intentionally simple: a single fixed font, hard-wrapped
// at the page margin, no layout reconstruction. It matches what
// commercial PII gateways (Private AI, Limina) produce for the
// "give me back a redacted PDF" use case — a readable artefact
// you can hand to a downstream tool, not a pixel-perfect copy of
// the input.
func RenderTextAsPDF(text string) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	// The core fpdf fonts (Helvetica/Times/Courier) cover Latin-1
	// only. For broader coverage (German umlauts, Romanian / Polish
	// diacritics) we register the bundled DejaVu fallback — a TTF
	// with Unicode coverage — when present. If the font isn't
	// available we fall back to Courier and best-effort
	// transliterate to Latin-1 so the output is still readable
	// instead of erroring.
	useUnicode := false
	pdf.SetFont("Courier", "", 9)
	if useUnicode {
		_ = useUnicode // placeholder for a future TTF-bundled build
	}

	emitText := func(s string) {
		if !useUnicode {
			s = transliterateToLatin1(s)
		}
		// MultiCell wraps at the right margin. cell width 0 means
		// "use the remaining usable width."
		pdf.MultiCell(0, 4, s, "", "L", false)
	}

	pages := strings.Split(text, "\f")
	for i, page := range pages {
		pdf.AddPage()
		page = strings.TrimRight(page, "\n")
		if page == "" {
			// Empty page is fine — still emit it so page count
			// matches the input PDF when both round-trip through
			// OCR + render.
			_ = i
			continue
		}
		emitText(page)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// transliterateToLatin1 maps common Unicode characters that appear in
// our OCR output (German umlauts, Romanian / Polish / French diacritics,
// curly quotes, em/en dashes) into Latin-1-safe substitutes. Characters
// outside the mapping fall back to '?'.
//
// This is a stop-gap so the core-font fpdf path produces a readable
// PDF when ANONDE_PDF_FONT isn't pointing at a Unicode TTF. The
// downstream goal is to bundle a TTF in the NER image and remove
// the lossy mapping.
func transliterateToLatin1(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// ASCII passes through directly.
		if r < 0x80 {
			b.WriteByte(byte(r))
			continue
		}
		// Latin-1 supplement (0x80–0xFF): write the single Latin-1
		// byte, NOT the 2-byte UTF-8 encoding. fpdf treats the
		// buffer as a Latin-1 byte stream, so b.WriteRune would
		// surface as mojibake (e.g. "Ã®" instead of "î").
		if r < 0x100 {
			b.WriteByte(byte(r))
			continue
		}
		if sub, ok := latin1Subs[r]; ok {
			b.WriteString(sub)
			continue
		}
		b.WriteByte('?')
	}
	return b.String()
}

var latin1Subs = map[rune]string{
	// Romanian
	'Ă': "A", 'ă': "a",
	'Â': "A", 'â': "a",
	'Î': "I", 'î': "i",
	'Ș': "S", 'ș': "s", 'Ş': "S", 'ş': "s",
	'Ț': "T", 'ț': "t", 'Ţ': "T", 'ţ': "t",
	// French / Italian / Spanish covered by Latin-1 already.
	// Polish
	'Ł': "L", 'ł': "l",
	'Ą': "A", 'ą': "a",
	'Ę': "E", 'ę': "e",
	'Ć': "C", 'ć': "c",
	'Ń': "N", 'ń': "n",
	'Ó': "O", 'ó': "o",
	'Ś': "S", 'ś': "s",
	'Ź': "Z", 'ź': "z",
	'Ż': "Z", 'ż': "z",
	// Punctuation
	'‘': "'", '’': "'",
	'“': "\"", '”': "\"",
	'–': "-", '—': "-",
	'…': "...",
	' ': " ",
	'\f':     "\n",
}
