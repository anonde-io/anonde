//go:build ner

package recognizers

import "strings"

// ner_shared.go holds the small model-agnostic helpers shared by the
// GLiNER recognizers (span + flat decoders, and their pools/ensemble):
// sliding-window chunking, candidate-span bookkeeping, and the
// model-name → cache-dir sanitiser. They live in their own file so the
// span and flat recognizers can reuse them without duplication.

// nerCand is an internal candidate span produced by a model before
// type-grouped overlap dedup.
type nerCand struct {
	start, end int
	score      float64
	typ        string
}

// nerChunk is a slice of input text fed to the model. ByteStart is its
// offset in the original document so model-relative entity positions can
// be lifted back to global offsets.
type nerChunk struct {
	ByteStart int
	Text      string
}

// chunkForNER splits text into chunks of at most chunkChars bytes, breaking
// on the latest newline-or-space before the limit. Adjacent chunks overlap
// by overlapChars to catch entities sitting near chunk boundaries.
//
// All cuts are made at ASCII whitespace bytes (' ' or '\n'), which are
// always at rune boundaries; so the returned chunks are valid UTF-8 even
// when the input contains multi-byte runes like ä/ö/ü/ß.
//
// If text is short enough to fit in one chunk, returns a single chunk
// containing the whole text (and the recognizer behaves identically to a
// non-chunking implementation).
func chunkForNER(text string, chunkChars, overlapChars int) []nerChunk {
	n := len(text)
	if n <= chunkChars || chunkChars <= 0 {
		return []nerChunk{{ByteStart: 0, Text: text}}
	}
	if overlapChars < 0 || overlapChars >= chunkChars {
		overlapChars = 0
	}

	var out []nerChunk
	start := 0
	for start < n {
		end := start + chunkChars
		if end >= n {
			out = append(out, nerChunk{ByteStart: start, Text: text[start:]})
			break
		}
		// Prefer the last newline, fall back to the last space within
		// the window. Both are single-byte ASCII so end+1 is always a
		// rune boundary.
		if idx := strings.LastIndex(text[start:end], "\n"); idx > 0 {
			end = start + idx + 1
		} else if idx := strings.LastIndex(text[start:end], " "); idx > 0 {
			end = start + idx + 1
		} else {
			// No whitespace anywhere in this window; exotic in
			// natural text. Back up to a rune boundary so the slice
			// stays valid UTF-8.
			for end > start && (text[end]&0xC0) == 0x80 {
				end--
			}
		}
		out = append(out, nerChunk{ByteStart: start, Text: text[start:end]})

		// Step forward by (chunkChars - overlap), then snap to the
		// next whitespace for rune-safety and so we don't begin a
		// chunk mid-word.
		// No useful overlap if the stride would land past chunk end.
		nextStart := min(start+chunkChars-overlapChars, end)
		for nextStart < n && text[nextStart] != ' ' && text[nextStart] != '\n' {
			nextStart++
		}
		for nextStart < n && (text[nextStart] == ' ' || text[nextStart] == '\n') {
			nextStart++
		}
		if nextStart <= start {
			nextStart = end // ensure forward progress
		}
		start = nextStart
	}
	return out
}

// sanitizeModelName converts a HuggingFace model ID (e.g.
// "knowledgator/gliner-pii-base-v1.0") to the local directory name the
// hugot library's downloader uses internally; slashes become underscores.
// Keep this in lock-step with hugot's downloader; if the upstream
// convention changes, prefer the path returned by hugot.DownloadModel.
func sanitizeModelName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
