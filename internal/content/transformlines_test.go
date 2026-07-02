package content

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// transformLinesReference is the ORIGINAL strings.SplitAfter-based body of
// TransformLines, frozen as the oracle for the streaming rewrite. The
// streaming version must reproduce its output byte-for-byte.
func transformLinesReference(content string, forceJSON bool, jsonFn, textFn func(string) (string, error)) (string, error) {
	if content == "" {
		return content, nil
	}
	var out strings.Builder
	out.Grow(len(content))
	for _, raw := range strings.SplitAfter(content, "\n") {
		nl := ""
		line := raw
		if strings.HasSuffix(line, "\n") {
			nl = "\n"
			line = line[:len(line)-1]
		}
		if line == "" {
			out.WriteString(nl)
			continue
		}
		cleaned := SanitizeUTF8(StripANSI(line))
		var (
			processed string
			err       error
		)
		trimmed := strings.TrimSpace(cleaned)
		looksJSON := len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
		if looksJSON {
			processed, err = jsonFn(cleaned)
			if err != nil {
				if forceJSON {
					return "", fmt.Errorf("ndjson line is not valid JSON: %w", err)
				}
				processed, err = textFn(cleaned)
			}
		} else if forceJSON {
			return "", fmt.Errorf("ndjson line does not start with { or [: %q", trimmed)
		} else {
			processed, err = textFn(cleaned)
		}
		if err != nil {
			return "", err
		}
		out.WriteString(processed)
		out.WriteString(nl)
	}
	return out.String(), nil
}

// TestTransformLines_ByteIdenticalToReference fuzzes tricky newline / empty /
// ANSI / JSON-looking line patterns through both the streaming TransformLines
// and the frozen SplitAfter oracle, asserting identical output and errors.
func TestTransformLines_ByteIdenticalToReference(t *testing.T) {
	// Deterministic transforms that make branch selection observable.
	textFn := func(s string) (string, error) { return "T[" + strings.ToUpper(s) + "]", nil }
	jsonFn := func(s string) (string, error) { return "J[" + s + "]", nil }

	rng := rand.New(rand.NewSource(0xC0FFEE))
	// Chunks include newlines, whitespace-only lines, JSON-openers, ANSI
	// escapes, and multibyte runes so every branch and terminator case fires.
	chunks := []string{
		"\n", "\n\n", "abc", "  ", "\t", "{\"k\":1}", "[1,2]", "{not json",
		"\x1b[31mred\x1b[0m", "日本語", "", "line", "{", "[", "x\ny",
	}

	build := func() string {
		var b strings.Builder
		n := rng.Intn(8)
		for i := 0; i < n; i++ {
			b.WriteString(chunks[rng.Intn(len(chunks))])
		}
		return b.String()
	}

	for iter := 0; iter < 3000; iter++ {
		content := build()
		got, gotErr := TransformLines(content, false, jsonFn, textFn)
		want, wantErr := transformLinesReference(content, false, jsonFn, textFn)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("iter %d: err mismatch got=%v want=%v content=%q", iter, gotErr, wantErr, content)
		}
		if got != want {
			t.Fatalf("iter %d: output mismatch\n got %q\nwant %q\ncontent=%q", iter, got, want, content)
		}
	}
}

// TestTransformLines_ForceJSONParity checks the NDJSON (forceJSON=true) path
// stays identical, including the non-JSON-line error, under the rewrite.
func TestTransformLines_ForceJSONParity(t *testing.T) {
	jsonFn := func(s string) (string, error) { return "J[" + s + "]", nil }
	textFn := func(s string) (string, error) { return s, nil }

	cases := []string{
		"{\"a\":1}\n{\"b\":2}\n",
		"{\"a\":1}\n{\"b\":2}",
		"{\"only\":true}",
		"plain text line", // not JSON -> error under forceJSON
		"",
		"\n\n",
	}
	for _, content := range cases {
		got, gotErr := TransformLines(content, true, jsonFn, textFn)
		want, wantErr := transformLinesReference(content, true, jsonFn, textFn)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("content=%q: err mismatch got=%v want=%v", content, gotErr, wantErr)
		}
		if got != want {
			t.Fatalf("content=%q: output mismatch got=%q want=%q", content, got, want)
		}
	}
}
