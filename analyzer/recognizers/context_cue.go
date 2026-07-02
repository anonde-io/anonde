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
