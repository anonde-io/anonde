package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	pdf "github.com/ledongthuc/pdf"
)

const (
	contentFormatText = "text"
	contentFormatJSON = "json"
	contentFormatPDF  = "pdf"
	contentFormatAuto = "auto"
)

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
	default:
		return ""
	}
}

func resolveAutoContentFormat(content string) string {
	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err == nil {
		return contentFormatJSON
	}
	return contentFormatText
}

func extractAnalyzableText(content, format string) (string, error) {
	switch format {
	case contentFormatText, contentFormatJSON:
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
