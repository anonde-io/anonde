package platform

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

const (
	contentFormatText   = "text"
	contentFormatJSON   = "json"
	contentFormatPDF    = "pdf"
	contentFormatAuto   = "auto"
	contentFormatNDJSON = "ndjson"
	contentFormatLogs   = "logs"
)

// ansiEscapeRegexp matches CSI / OSC / ESC sequences emitted by terminals.
// Server logs piped from journals or Docker frequently include these.
var ansiEscapeRegexp = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

func normalizeContentFormat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", contentFormatText:
		return contentFormatText
	case contentFormatJSON:
		return contentFormatJSON
	case contentFormatPDF:
		return contentFormatPDF
	case contentFormatAuto:
		return contentFormatAuto
	case contentFormatNDJSON, "jsonl", "json-lines":
		return contentFormatNDJSON
	case contentFormatLogs, "log":
		return contentFormatLogs
	default:
		return ""
	}
}

func resolveAutoContentFormat(content string) string {
	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err == nil {
		return contentFormatJSON
	}
	if isNDJSON(content) {
		return contentFormatNDJSON
	}
	return contentFormatText
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

// stripANSI removes ANSI escape sequences. Safe to call on any input — leaves
// printable text untouched.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	return ansiEscapeRegexp.ReplaceAllString(s, "")
}

// sanitizeUTF8 replaces invalid UTF-8 byte sequences with the Unicode
// replacement character. The pattern engine and prose NER both assume valid
// UTF-8 input.
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

func extractAnalyzableText(content, format string) (string, error) {
	switch format {
	case contentFormatText, contentFormatJSON, contentFormatNDJSON, contentFormatLogs:
		return content, nil
	case contentFormatPDF:
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

func transformJSONStringLeaves(content string, fn func(string) (string, error)) (string, error) {
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

// transformLines splits content on \n, applies fn to each line, and reassembles
// preserving line terminators. Each line is independently sanitized (ANSI strip
// + valid UTF-8). Empty lines pass through untouched.
//
// The forceJSON flag controls whether each non-empty line MUST parse as JSON
// (NDJSON behavior); when false, lines are auto-classified per line as
// JSON-or-text (logs behavior).
func transformLines(content string, forceJSON bool, jsonFn, textFn func(string) (string, error)) (string, error) {
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
		cleaned := sanitizeUTF8(stripANSI(line))
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
