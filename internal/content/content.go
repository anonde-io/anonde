// Package content owns the I/O-layer concerns of the platform: content
// format parsing (text / json / ndjson / logs / pdf / auto), JSON
// recursion through string leaves, line-oriented log handling, ANSI
// stripping, UTF-8 sanitisation. Everything in here is pure and has no
// dependency on anonde's analyzer or anonymizer.
//
// The package was extracted out of internal/platform during the
// internal/{api,core,content,store} split — both core (Service) and
// any future caller can use it without pulling transport, storage, or
// the analyzer.
package content

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"
)

// Content format identifiers, used as the wire value of
// AnalyzerRequest.ContentFormat and to drive the per-format branches
// in Service.Ingest / Service.Reveal / Service.Synthesize.
const (
	FormatText   = "text"
	FormatJSON   = "json"
	FormatPDF    = "pdf"
	FormatAuto   = "auto"
	FormatNDJSON = "ndjson"
	FormatLogs   = "logs"
)

// ansiEscapeRegexp matches CSI / OSC / ESC sequences emitted by terminals.
// Server logs piped from journals or Docker frequently include these.
var ansiEscapeRegexp = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// NormalizeFormat maps an inbound content_format string to one of the
// FormatX constants. Returns "" for unknown formats. Accepts a handful
// of common aliases (jsonl/json-lines for ndjson, log for logs).
func NormalizeFormat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", FormatText:
		return FormatText
	case FormatJSON:
		return FormatJSON
	case FormatPDF:
		return FormatPDF
	case FormatAuto:
		return FormatAuto
	case FormatNDJSON, "jsonl", "json-lines":
		return FormatNDJSON
	case FormatLogs, "log":
		return FormatLogs
	default:
		return ""
	}
}

// ResolveAutoFormat inspects the content and picks one of FormatJSON,
// FormatNDJSON, or FormatText. Called when the request asks for
// FormatAuto.
func ResolveAutoFormat(content string) string {
	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err == nil {
		return FormatJSON
	}
	if isNDJSON(content) {
		return FormatNDJSON
	}
	return FormatText
}

// isNDJSON returns true when content has at least two non-empty lines and every
// non-empty line parses as JSON. Single-line JSON is handled by the json branch.
func isNDJSON(content string) bool {
	lines := strings.Split(content, "\n")
	nonEmpty := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		var doc any
		if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
			return false
		}
		nonEmpty++
	}
	return nonEmpty >= 2
}

// StripANSI removes ANSI escape sequences. Safe to call on any input —
// leaves printable text untouched.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	return ansiEscapeRegexp.ReplaceAllString(s, "")
}

// SanitizeUTF8 replaces invalid UTF-8 byte sequences with the Unicode
// replacement character. The pattern engine and prose NER both assume
// valid UTF-8 input.
func SanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

// ExtractAnalyzable returns the plain-text representation the analyzer
// can run over. Text / JSON / NDJSON / logs are pass-through; PDFs are
// base64-decoded and rendered to text page by page.
func ExtractAnalyzable(content, format string) (string, error) {
	switch format {
	case FormatText, FormatJSON, FormatNDJSON, FormatLogs:
		return content, nil
	case FormatPDF:
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return "", fmt.Errorf("decode base64 pdf content: %w", err)
		}
		reader := bytes.NewReader(raw)
		pdfReader, err := pdf.NewReader(reader, int64(len(raw)))
		if err != nil {
			return "", fmt.Errorf("read pdf content: %w", err)
		}
		var out strings.Builder
		total := pdfReader.NumPage()
		for pageNum := 1; pageNum <= total; pageNum++ {
			page := pdfReader.Page(pageNum)
			if page.V.IsNull() {
				continue
			}
			text, err := page.GetPlainText(nil)
			if err != nil && err != io.EOF {
				return "", fmt.Errorf("extract pdf page %d text: %w", pageNum, err)
			}
			if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(text)
		}
		return strings.TrimSpace(out.String()), nil
	default:
		return "", fmt.Errorf("unsupported content_format %q", format)
	}
}

// TransformJSONStringLeaves walks a JSON document and applies fn to every
// string leaf, returning the document re-serialised. Object keys,
// numbers, booleans, and null pass through unchanged.
func TransformJSONStringLeaves(content string, fn func(string) (string, error)) (string, error) {
	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", fmt.Errorf("parse json content: %w", err)
	}
	updated, err := transformJSONValue(doc, fn)
	if err != nil {
		return "", err
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(updated); err != nil {
		return "", fmt.Errorf("marshal transformed json: %w", err)
	}
	return strings.TrimSpace(encoded.String()), nil
}

func transformJSONValue(v any, fn func(string) (string, error)) (any, error) {
	switch typed := v.(type) {
	case map[string]any:
		next := make(map[string]any, len(typed))
		for key, value := range typed {
			out, err := transformJSONValue(value, fn)
			if err != nil {
				return nil, err
			}
			next[key] = out
		}
		return next, nil
	case []any:
		next := make([]any, len(typed))
		for i := range typed {
			out, err := transformJSONValue(typed[i], fn)
			if err != nil {
				return nil, err
			}
			next[i] = out
		}
		return next, nil
	case string:
		return fn(typed)
	default:
		return typed, nil
	}
}

// TransformLines splits content on \n, applies fn to each line, and
// reassembles preserving line terminators. Each line is independently
// sanitised (ANSI strip + valid UTF-8). Empty lines pass through
// untouched.
//
// The forceJSON flag controls whether each non-empty line MUST parse
// as JSON (NDJSON behavior); when false, lines are auto-classified
// per line as JSON-or-text (logs behavior).
func TransformLines(content string, forceJSON bool, jsonFn, textFn func(string) (string, error)) (string, error) {
	if content == "" {
		return content, nil
	}
	var out strings.Builder
	out.Grow(len(content))

	// strings.SplitAfter keeps the trailing \n on each line, which lets us
	// reassemble exactly without an off-by-one on the last line.
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
				// logs: fall through to text on JSON parse failure
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
