package recognizers

import (
	"strings"
	"testing"
)

// TestChunkCapError pins the H6 fix: a GLiNER input needing more chunks than
// the active cap must fail closed (so the analyzer rejects the request)
// rather than silently NER-covering only the head and leaking the tail's
// PERSON/ORG/LOC. Within-cap and disabled-cap inputs return nil.
func TestChunkCapError(t *testing.T) {
	cases := []struct {
		name                          string
		nChunks, maxChunks, textBytes int
		wantErr                       bool
	}{
		{"within cap", 10, 64, 12000, false},
		{"exactly at cap", 64, 64, 80000, false},
		{"one over cap fails closed", 65, 64, 82000, true},
		{"far over cap fails closed", 200, 64, 240000, true},
		{"cap disabled (negative) never errors", 5000, -1, 6_000_000, false},
		{"cap <= 0 is treated as no cap", 100, 0, 120000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := chunkCapError(tc.nChunks, tc.maxChunks, tc.textBytes)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected fail-closed error for %d chunks over cap %d, got nil", tc.nChunks, tc.maxChunks)
				}
				// The error must name the limit and the override knob so an
				// operator knows how to proceed.
				if !strings.Contains(err.Error(), "max_chunks") || !strings.Contains(err.Error(), "MaxChunks") {
					t.Errorf("error should name the cap and the override knob; got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error for %d chunks / cap %d: %v", tc.nChunks, tc.maxChunks, err)
			}
		})
	}
}
