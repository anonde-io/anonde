package platform

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pdfFixture reads the PDF fixture and returns its raw bytes.
func pdfFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "pii_sample.pdf"))
	if err != nil {
		t.Fatalf("read PDF fixture: %v", err)
	}
	return raw
}

// pdfFixtureB64 returns the fixture as a base64-encoded string (the wire
// format accepted by extractAnalyzableText for PDF content).
func pdfFixtureB64(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(pdfFixture(t))
}

// ---------------------------------------------------------------------------
// normalizeContentFormat
// ---------------------------------------------------------------------------

func TestNormalizeContentFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"", contentFormatText},
		{"text", contentFormatText},
		{"TEXT", contentFormatText},
		{"  Text  ", contentFormatText},
		{"json", contentFormatJSON},
		{"JSON", contentFormatJSON},
		{"pdf", contentFormatPDF},
		{"PDF", contentFormatPDF},
		{"auto", contentFormatAuto},
		{"AUTO", contentFormatAuto},
		{"xml", ""},
		{"binary", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeContentFormat(tc.input)
			if got != tc.want {
				t.Errorf("normalizeContentFormat(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveAutoContentFormat
// ---------------------------------------------------------------------------

func TestResolveAutoContentFormat_JSON(t *testing.T) {
	t.Parallel()
	inputs := []string{
		`{"key":"value"}`,
		`[1,2,3]`,
		`"just a string"`,
	}
	for _, in := range inputs {
		if got := resolveAutoContentFormat(in); got != contentFormatJSON {
			t.Errorf("resolveAutoContentFormat(%q) = %q, want %q", in, got, contentFormatJSON)
		}
	}
}

func TestResolveAutoContentFormat_Text(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"plain text",
		"not json at all",
		"",
		"{broken json",
	}
	for _, in := range inputs {
		if got := resolveAutoContentFormat(in); got != contentFormatText {
			t.Errorf("resolveAutoContentFormat(%q) = %q, want %q", in, got, contentFormatText)
		}
	}
}

// ---------------------------------------------------------------------------
// extractAnalyzableText — text / json pass-through
// ---------------------------------------------------------------------------

func TestExtractAnalyzableText_Text(t *testing.T) {
	t.Parallel()
	const input = "hello world"
	got, err := extractAnalyzableText(input, contentFormatText)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestExtractAnalyzableText_JSON(t *testing.T) {
	t.Parallel()
	const input = `{"name":"Alice"}`
	got, err := extractAnalyzableText(input, contentFormatJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestExtractAnalyzableText_UnknownFormat(t *testing.T) {
	t.Parallel()
	_, err := extractAnalyzableText("anything", "xml")
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Errorf("expected format name in error, got: %v", err)
	}
}

func TestExtractAnalyzableText_PDF_InvalidBase64(t *testing.T) {
	t.Parallel()
	_, err := extractAnalyzableText("not-valid-base64!!!", contentFormatPDF)
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestExtractAnalyzableText_PDF_NotAPDF(t *testing.T) {
	t.Parallel()
	garbage := base64.StdEncoding.EncodeToString([]byte("this is not a pdf"))
	_, err := extractAnalyzableText(garbage, contentFormatPDF)
	if err == nil {
		t.Fatal("expected error for non-PDF bytes, got nil")
	}
}

// ---------------------------------------------------------------------------
// extractAnalyzableText — PDF fixture
// ---------------------------------------------------------------------------

func TestExtractAnalyzableText_PDF_ExtractsText(t *testing.T) {
	t.Parallel()
	text, err := extractAnalyzableText(pdfFixtureB64(t), contentFormatPDF)
	if err != nil {
		t.Fatalf("extractAnalyzableText: %v", err)
	}
	if text == "" {
		t.Fatal("expected non-empty text from PDF, got empty string")
	}

	wantSubstrings := []string{
		"Alice Johnson",
		"alice@example.com",
		"+1-800-555-0199",
		"123-45-6789",
		"Acme Corp",
		"New York",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(text, want) {
			t.Errorf("extracted text missing %q\nfull text: %q", want, text)
		}
	}
}

func TestExtractAnalyzableText_PDF_MultiPage(t *testing.T) {
	t.Parallel()
	// Build a two-page PDF from two copies of the fixture stitched together
	// at the object level. Instead, generate a fresh two-page raw PDF inline.
	twoPagePDF := buildTwoPagePDF(t)
	b64 := base64.StdEncoding.EncodeToString(twoPagePDF)

	text, err := extractAnalyzableText(b64, contentFormatPDF)
	if err != nil {
		t.Fatalf("extractAnalyzableText: %v", err)
	}
	// Both pages' sentinel strings must appear.
	if !strings.Contains(text, "PageOne") {
		t.Errorf("page 1 content missing from extracted text: %q", text)
	}
	if !strings.Contains(text, "PageTwo") {
		t.Errorf("page 2 content missing from extracted text: %q", text)
	}
}

// buildTwoPagePDF creates a minimal two-page PDF entirely in memory for testing.
func buildTwoPagePDF(t *testing.T) []byte {
	t.Helper()

	makeStream := func(line string) string {
		return "BT\n/F1 12 Tf\n72 720 Td\n(" + line + ") Tj\nET\n"
	}

	s1 := makeStream("PageOne sentinel text")
	s2 := makeStream("PageTwo sentinel text")

	font := "5 0 obj\n<</Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding>>\nendobj\n"
	o1 := "1 0 obj\n<</Type /Catalog /Pages 2 0 R>>\nendobj\n"
	o2 := "2 0 obj\n<</Type /Pages /Kids [3 0 R 6 0 R] /Count 2>>\nendobj\n"
	resources := "<</Font <</F1 5 0 R>>>>"
	o3 := "3 0 obj\n<</Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources " + resources + ">>\nendobj\n"
	o4 := "4 0 obj\n<</Length " + itoa(len(s1)) + ">>\nstream\n" + s1 + "endstream\nendobj\n"
	o6 := "6 0 obj\n<</Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources " + resources + ">>\nendobj\n"
	o7 := "7 0 obj\n<</Length " + itoa(len(s2)) + ">>\nstream\n" + s2 + "endstream\nendobj\n"

	header := "%PDF-1.4\n"
	off := make([]int, 8)
	pos := len(header)
	bodies := []string{"", o1, o2, o3, o4, font, o6, o7}
	for i := 1; i <= 7; i++ {
		off[i] = pos
		pos += len(bodies[i])
	}
	xrefStart := pos

	xref := "xref\n0 8\n0000000000 65535 f \n"
	for i := 1; i <= 7; i++ {
		xref += padZero(off[i], 10) + " 00000 n \n"
	}
	trailer := "trailer\n<</Size 8 /Root 1 0 R>>\nstartxref\n" + itoa(xrefStart) + "\n%%EOF\n"

	return []byte(header + o1 + o2 + o3 + o4 + font + o6 + o7 + xref + trailer)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func padZero(n, width int) string {
	s := itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// ---------------------------------------------------------------------------
// transformJSONStringLeaves
// ---------------------------------------------------------------------------

func TestTransformJSONStringLeaves_FlatObject(t *testing.T) {
	t.Parallel()
	input := `{"name":"Alice","city":"New York"}`
	got, err := transformJSONStringLeaves(input, func(s string) (string, error) {
		return strings.ToUpper(s), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `"ALICE"`) || !strings.Contains(got, `"NEW YORK"`) {
		t.Errorf("unexpected output: %s", got)
	}
}

func TestTransformJSONStringLeaves_NestedObject(t *testing.T) {
	t.Parallel()
	input := `{"person":{"name":"Bob","age":30}}`
	got, err := transformJSONStringLeaves(input, func(s string) (string, error) {
		return "[" + s + "]", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `"[Bob]"`) {
		t.Errorf("expected nested string transformed, got: %s", got)
	}
	// Non-string values (age:30) must pass through unchanged.
	if !strings.Contains(got, `"age":30`) {
		t.Errorf("expected numeric value unchanged, got: %s", got)
	}
}

func TestTransformJSONStringLeaves_Array(t *testing.T) {
	t.Parallel()
	input := `["alpha","beta","gamma"]`
	got, err := transformJSONStringLeaves(input, func(s string) (string, error) {
		return s + "!", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"alpha!", "beta!", "gamma!"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got: %s", want, got)
		}
	}
}

func TestTransformJSONStringLeaves_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := transformJSONStringLeaves("{broken", func(s string) (string, error) {
		return s, nil
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
