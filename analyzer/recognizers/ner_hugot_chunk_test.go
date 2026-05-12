//go:build hugot

package recognizers

import (
	"strings"
	"testing"
)

// Internal tests for the sliding-window chunking used by the hugot NER
// recognizer. The function under test is package-private, so this file
// stays in package recognizers (not recognizers_test).

func TestChunkForNER_ShortTextNotChunked(t *testing.T) {
	t.Parallel()
	chunks := chunkForNER("hello world", 1500, 200)
	if len(chunks) != 1 {
		t.Fatalf("short text should produce 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ByteStart != 0 || chunks[0].Text != "hello world" {
		t.Errorf("unexpected chunk: %+v", chunks[0])
	}
}

func TestChunkForNER_LongTextSplits(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("Dies ist ein Beispielsatz Nummer ")
		b.WriteString(itoa(i))
		b.WriteString(".\n")
	}
	text := b.String()
	chunks := chunkForNER(text, 1500, 200)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for %d-byte text, got %d", len(text), len(chunks))
	}

	// Each chunk must be ≤ chunkChars + small slop.
	for i, c := range chunks {
		if len(c.Text) > 1600 {
			t.Errorf("chunk %d too long: %d bytes", i, len(c.Text))
		}
	}

	// First chunk starts at 0.
	if chunks[0].ByteStart != 0 {
		t.Errorf("first chunk must start at 0, got %d", chunks[0].ByteStart)
	}

	// Each chunk's text must equal text[ByteStart : ByteStart+len].
	for i, c := range chunks {
		if c.ByteStart < 0 || c.ByteStart+len(c.Text) > len(text) {
			t.Errorf("chunk %d offset out of range: start=%d end=%d text-len=%d", i, c.ByteStart, c.ByteStart+len(c.Text), len(text))
			continue
		}
		if text[c.ByteStart:c.ByteStart+len(c.Text)] != c.Text {
			t.Errorf("chunk %d ByteStart doesn't match text slice", i)
		}
	}

	// Last chunk should end at len(text).
	last := chunks[len(chunks)-1]
	if last.ByteStart+len(last.Text) != len(text) {
		t.Errorf("last chunk should end at text length: got end=%d, text-len=%d",
			last.ByteStart+len(last.Text), len(text))
	}

	// Forward progress.
	for i := 1; i < len(chunks); i++ {
		if chunks[i].ByteStart <= chunks[i-1].ByteStart {
			t.Errorf("chunk %d does not advance: start=%d, prev start=%d",
				i, chunks[i].ByteStart, chunks[i-1].ByteStart)
		}
	}
}

// Chunking on text containing multi-byte runes must produce chunks whose
// ByteStart is on a rune boundary, and chunk text slices must round-trip.
func TestChunkForNER_RuneSafe(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("Frau Müller Schäfer Mößler wohnt in München.\n")
	}
	text := b.String()
	chunks := chunkForNER(text, 1500, 200)

	for i, c := range chunks {
		if c.ByteStart > 0 && c.ByteStart < len(text) {
			if text[c.ByteStart]&0xC0 == 0x80 {
				t.Errorf("chunk %d ByteStart=%d is mid-rune", i, c.ByteStart)
			}
		}
		if c.Text != text[c.ByteStart:c.ByteStart+len(c.Text)] {
			t.Errorf("chunk %d text round-trip failed", i)
		}
	}
}

// itoa avoids importing strconv for the test fixture.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
