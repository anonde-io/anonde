// Package content owns the I/O-layer concerns of the anonde server: content
// format parsing (text / json / ndjson / logs / pdf / auto), JSON
// recursion through string leaves, line-oriented log handling, ANSI
// stripping, UTF-8 sanitisation. Everything in here is pure and has no
// dependency on anonde's analyzer or anonymizer.
//
// The package was extracted out of internal/api during the
// internal/{api,core,content,store} split; both core (Service) and
// any future caller can use it without pulling transport, storage, or
// the analyzer.
package content

import (
	"bytes"
	"context"
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

// StripANSI removes ANSI escape sequences. Safe to call on any input,
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
//
// The context bounds any OCR fallback (pdftoppm + tesseract) so a
// cancelled request tears down its external work; see OCRPDFBytes.
func ExtractAnalyzable(ctx context.Context, content, format string) (string, error) {
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
			// Some MFP-scanned PDFs have layouts ledongthuc/pdf can't
			// parse. Try OCR before giving up.
			if ocrText, ocrErr := OCRPDFBytes(ctx, raw); ocrErr == nil && ocrText != "" {
				return ocrText, nil
			}
			return "", fmt.Errorf("read pdf content: %w", err)
		}
		var out strings.Builder
		// Track the builder's trailing byte instead of materializing the whole
		// buffer with out.String() every page — the old HasSuffix check made
		// multi-page extraction near-quadratic. A page separator '\n' is only
		// written when the buffer is non-empty and doesn't already end in one.
		lastByteWasNewline := false
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
			if out.Len() > 0 && !lastByteWasNewline {
				out.WriteByte('\n')
				lastByteWasNewline = true
			}
			out.WriteString(text)
			if len(text) > 0 {
				lastByteWasNewline = text[len(text)-1] == '\n'
			}
		}
		extracted := strings.TrimSpace(out.String())
		// Scanned PDFs (image-only, no text layer) come back empty or
		// near-empty here. Fall back to OCR so the analyzer has
		// something to see. The OCR helper is a no-op when pdftoppm /
		// tesseract aren't installed, so this is safe in the
		// patterns-only image too.
		if len(extracted) < ocrTextFloor() {
			if ocrText, err := OCRPDFBytes(ctx, raw); err == nil && ocrText != "" {
				return ocrText, nil
			}
		}
		return extracted, nil
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

// TransformJSONStringLeavesAt is the offset-aware sibling of
// TransformJSONStringLeaves. It walks the document in source order and,
// for every string VALUE leaf, invokes fn with the byte offset (into
// content) at which that leaf's decoded content begins — i.e. the byte
// immediately after the leaf's opening quote. Object keys, numbers,
// booleans and null pass through unchanged, exactly as
// TransformJSONStringLeaves, and the re-serialised output is identical.
//
// The reported base is exact for string leaves that contain no escape
// sequences (the common case): base + a leaf-local finding offset yields
// the finding's byte position in content. For leaves that use JSON
// escapes the base still locates the leaf, but a per-character mapping
// past an escape drifts by the escape's extra source bytes.
//
// Duplicate object keys follow JSON last-wins: only the retained (final)
// value for a key is passed to fn. A value shadowed by a later duplicate
// key is dropped before fn runs, so it yields no callback — and none of
// fn's side effects (e.g. the vault writes / finding+token accumulation in
// Service.Ingest / Synthesize). This keeps both the re-serialised output and
// the set of fn calls in lock-step with the non-offset sibling
// TransformJSONStringLeaves, which dedups via json.Unmarshal's map before it
// walks. fn is called on retained leaves in source order.
func TransformJSONStringLeavesAt(content string, fn func(value string, base int) (string, error)) (string, error) {
	w := &jsonLeafWalker{src: content, dec: json.NewDecoder(strings.NewReader(content)), fn: fn}
	tree, err := w.parseValue()
	if err != nil {
		return "", err
	}
	// Reject trailing data after the top-level value so this matches
	// json.Unmarshal's single-document strictness (relied on by the
	// NDJSON per-line parse and by ResolveAutoFormat's classification).
	if _, err := w.dec.Token(); err != io.EOF {
		if err != nil {
			return "", fmt.Errorf("parse json content: %w", err)
		}
		return "", fmt.Errorf("parse json content: unexpected data after top-level value")
	}
	// parseValue only builds the lazy tree (buffering the last-wins value per
	// object key); fn runs here, once per retained leaf in source order, so a
	// shadowed duplicate never fires it. Resolving only after the trailing-data
	// check also means an invalid document triggers no callbacks at all.
	updated, err := w.resolve(tree)
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

// jsonLeafWalker decodes a JSON document with a streaming json.Decoder in two
// phases. parseValue reports each string leaf's source position via
// Decoder.InputOffset and builds a lazy tree (pendingLeaf / pendingObj /
// pendingArr / scalar) that carries those offsets; resolve then applies fn and
// rebuilds the same map[string]any / []any / string / float64 / bool / nil
// tree json.Unmarshal produces (so re-serialisation is byte-identical to
// TransformJSONStringLeaves). Deferring fn to resolve is what lets an object
// drop a value shadowed by a later duplicate key before it ever reaches fn.
type jsonLeafWalker struct {
	src string
	dec *json.Decoder
	fn  func(value string, base int) (string, error)
}

// pendingLeaf is a not-yet-transformed JSON string value plus the byte offset
// in src where its decoded content begins. fn is applied lazily in resolve,
// never at parse time, so a leaf shadowed by a later duplicate object key
// (JSON is last-wins) is dropped from the tree before resolve and never
// reaches fn — no callback, hence none of fn's side effects.
type pendingLeaf struct {
	value string
	base  int
}

// pendingObj is a JSON object captured in first-occurrence key order with
// last-wins values. Keeping keys in a slice makes resolve (and therefore fn's
// call order) follow source order in the common no-duplicate case, matching
// the pre-lazy behaviour; vals holds only the retained node per key.
type pendingObj struct {
	keys []string
	vals map[string]any
}

// pendingArr is a JSON array of pending element nodes, resolved in order.
type pendingArr struct {
	elems []any
}

// parseValue reads exactly one JSON value from the decoder into a lazy tree
// node WITHOUT invoking fn. Decode/syntax errors are wrapped with the "parse
// json content" prefix. fn (and its unwrapped errors) is deferred to resolve.
func (w *jsonLeafWalker) parseValue() (any, error) {
	before := int(w.dec.InputOffset())
	tok, err := w.dec.Token()
	if err != nil {
		return nil, wrapJSONParse(err)
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := &pendingObj{vals: map[string]any{}}
			for w.dec.More() {
				keyTok, err := w.dec.Token()
				if err != nil {
					return nil, wrapJSONParse(err)
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, wrapJSONParse(fmt.Errorf("object key is not a string"))
				}
				val, err := w.parseValue()
				if err != nil {
					return nil, err
				}
				// Last-wins: a duplicate key overwrites its buffered value,
				// dropping the earlier node (and every pending leaf under it)
				// so resolve never applies fn to a shadowed value. First
				// occurrence fixes the key's slot in source order.
				if _, dup := obj.vals[key]; !dup {
					obj.keys = append(obj.keys, key)
				}
				obj.vals[key] = val
			}
			if _, err := w.dec.Token(); err != nil { // closing '}'
				return nil, wrapJSONParse(err)
			}
			return obj, nil
		case '[':
			arr := &pendingArr{}
			for w.dec.More() {
				val, err := w.parseValue()
				if err != nil {
					return nil, err
				}
				arr.elems = append(arr.elems, val)
			}
			if _, err := w.dec.Token(); err != nil { // closing ']'
				return nil, wrapJSONParse(err)
			}
			return arr, nil
		default:
			return nil, wrapJSONParse(fmt.Errorf("unexpected delimiter %q", t))
		}
	case string:
		// The token span [before,after) covers optional separators plus the
		// quoted string; the opening quote is the first '"' in it and the
		// decoded content starts one byte later.
		after := int(w.dec.InputOffset())
		base := before
		if rel := strings.IndexByte(w.src[before:after], '"'); rel >= 0 {
			base = before + rel + 1
		}
		return &pendingLeaf{value: t, base: base}, nil
	default: // float64, bool, nil
		return t, nil
	}
}

// resolve walks the lazy tree parseValue produced and applies fn to every
// retained string leaf, returning the json.Unmarshal-shaped tree. Objects are
// walked in first-occurrence key order and arrays in element order, so fn runs
// in source order for documents without duplicate keys. fn errors propagate
// unwrapped so callers can tell an analyzer failure apart from a parse error.
func (w *jsonLeafWalker) resolve(node any) (any, error) {
	switch n := node.(type) {
	case *pendingLeaf:
		return w.fn(n.value, n.base)
	case *pendingObj:
		out := make(map[string]any, len(n.keys))
		for _, key := range n.keys {
			val, err := w.resolve(n.vals[key])
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	case *pendingArr:
		out := make([]any, len(n.elems))
		for i, elem := range n.elems {
			val, err := w.resolve(elem)
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	default: // float64, bool, nil
		return node, nil
	}
}

func wrapJSONParse(err error) error {
	return fmt.Errorf("parse json content: %w", err)
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

	// Stream over newline indexes one line at a time instead of allocating a
	// full []string via strings.SplitAfter. Each iteration isolates the line
	// (without its terminator) plus the terminator to re-append, matching
	// SplitAfter's semantics exactly — including the trailing empty segment
	// when content ends in '\n', so reassembly stays byte-for-byte identical.
	for i := 0; ; {
		rel := strings.IndexByte(content[i:], '\n')
		var line, nl string
		if rel < 0 {
			line = content[i:]
		} else {
			line = content[i : i+rel]
			nl = "\n"
		}

		if line == "" {
			out.WriteString(nl)
		} else {
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

		if rel < 0 {
			break
		}
		i += rel + 1
	}
	return out.String(), nil
}

// TransformLinesAt is the offset-aware sibling of TransformLines. It
// behaves identically (same splitting, sanitisation, JSON/text branch
// selection and error surface) but additionally passes each line's byte
// offset in content to the callbacks, so callers can turn line-local
// offsets into document-relative ones.
//
// The reported base is the offset of the RAW line start in content. It is
// exact for lines that carry no ANSI escapes or invalid UTF-8 (the common
// case); when SanitizeUTF8/StripANSI rewrite the line, base still points at
// the line start but per-byte offsets into the cleaned line drift by the
// removed/replaced bytes.
func TransformLinesAt(content string, forceJSON bool, jsonFn, textFn func(line string, base int) (string, error)) (string, error) {
	if content == "" {
		return content, nil
	}
	var out strings.Builder
	out.Grow(len(content))

	for i := 0; ; {
		rel := strings.IndexByte(content[i:], '\n')
		var line, nl string
		lineStart := i
		if rel < 0 {
			line = content[i:]
		} else {
			line = content[i : i+rel]
			nl = "\n"
		}

		if line == "" {
			out.WriteString(nl)
		} else {
			cleaned := SanitizeUTF8(StripANSI(line))
			var (
				processed string
				err       error
			)
			trimmed := strings.TrimSpace(cleaned)
			looksJSON := len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
			if looksJSON {
				processed, err = jsonFn(cleaned, lineStart)
				if err != nil {
					if forceJSON {
						return "", fmt.Errorf("ndjson line is not valid JSON: %w", err)
					}
					// logs: fall through to text on JSON parse failure
					processed, err = textFn(cleaned, lineStart)
				}
			} else if forceJSON {
				return "", fmt.Errorf("ndjson line does not start with { or [: %q", trimmed)
			} else {
				processed, err = textFn(cleaned, lineStart)
			}
			if err != nil {
				return "", err
			}
			out.WriteString(processed)
			out.WriteString(nl)
		}

		if rel < 0 {
			break
		}
		i += rel + 1
	}
	return out.String(), nil
}
