package recognizers

import "strings"

func hasLocalContextCue(text string, start, end, beforeChars, afterChars int, cues []string) bool {
	if text == "" || len(cues) == 0 {
		return false
	}
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	lo := start - beforeChars
	if lo < 0 {
		lo = 0
	}
	hi := end + afterChars
	if hi > len(text) {
		hi = len(text)
	}
	window := strings.ToLower(text[lo:hi])
	for _, cue := range cues {
		if hasContextCue(window, strings.ToLower(cue)) {
			return true
		}
	}
	return false
}

// hasLocalContextCueExcludingSelf reports whether any cue occurs in the two
// side-windows STRICTLY around text[start:end] — the candidate span itself is
// EXCLUDED so a token whose own surface contains a cue word cannot
// self-corroborate. It scans [start-beforeChars, start) and [end, end+afterChars)
// independently; a cue must sit wholly in one side-window to count. Use this
// (not hasLocalContextCue) whenever the gated span could itself be a cue —
// e.g. a `#social` hashtag or a dotted `github.actions` handle.
func hasLocalContextCueExcludingSelf(text string, start, end, beforeChars, afterChars int, cues []string) bool {
	if text == "" || len(cues) == 0 {
		return false
	}
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start > end {
		return false
	}
	// Before-window: [start-beforeChars, start) (afterChars=0 keeps hi at start).
	if hasLocalContextCue(text, start, start, beforeChars, 0, cues) {
		return true
	}
	// After-window: [end, end+afterChars) (beforeChars=0 keeps lo at end).
	return hasLocalContextCue(text, end, end, 0, afterChars, cues)
}

func hasContextCue(window, cue string) bool {
	if cue == "" {
		return false
	}
	if strings.ContainsAny(cue, " \t\n\r") {
		return strings.Contains(window, cue)
	}
	for offset := 0; offset < len(window); {
		idx := strings.Index(window[offset:], cue)
		if idx < 0 {
			return false
		}
		idx += offset
		if isCueBoundary(window, idx-1) && isCueBoundary(window, idx+len(cue)) {
			return true
		}
		offset = idx + len(cue)
	}
	return false
}

func isCueBoundary(s string, idx int) bool {
	if idx < 0 || idx >= len(s) {
		return true
	}
	b := s[idx]
	return !((b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_')
}
