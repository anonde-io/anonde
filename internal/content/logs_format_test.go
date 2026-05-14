package content

import (
	"strings"
	"testing"
)

// Tests for the line-oriented formats added on top of the original
// text/json/pdf set: ndjson + logs aliases, NDJSON auto-detection,
// ANSI stripping, and invalid-UTF-8 sanitisation. These exercise the
// helper surface that Service.Ingest / Service.Reveal lean on when
// processing log streams.

func TestNormalizeContentFormat_NewFormats(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"ndjson":     FormatNDJSON,
		"NDJSON":     FormatNDJSON,
		"jsonl":      FormatNDJSON,
		"json-lines": FormatNDJSON,
		"logs":       FormatLogs,
		"log":        FormatLogs,
	}
	for in, want := range cases {
		if got := NormalizeFormat(in); got != want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveAutoContentFormat_NDJSON(t *testing.T) {
	t.Parallel()
	in := `{"a":1}` + "\n" + `{"b":2}` + "\n"
	if got := ResolveAutoFormat(in); got != FormatNDJSON {
		t.Errorf("expected ndjson, got %q", got)
	}
}

func TestStripANSI_RemovesEscapes(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mERROR\x1b[0m something happened"
	got := StripANSI(in)
	if got != "ERROR something happened" {
		t.Errorf("expected escapes removed, got %q", got)
	}
}

func TestSanitizeUTF8_ReplacesInvalid(t *testing.T) {
	t.Parallel()
	// "abc" + invalid byte 0xff + "def"
	in := "abc\xffdef"
	got := SanitizeUTF8(in)
	if !strings.Contains(got, "abc") || !strings.Contains(got, "def") {
		t.Errorf("expected valid surrounding text preserved, got %q", got)
	}
	if strings.ContainsRune(got, 0xff) {
		t.Errorf("expected invalid byte to be removed, got %q", got)
	}
}
