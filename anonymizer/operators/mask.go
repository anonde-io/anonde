package operators

// Mask replaces characters in the PII with a masking character.
type Mask struct {
	// MaskingChar is the character to use (default: '*').
	MaskingChar string
	// CharsToMask is how many characters to mask. 0 means all.
	CharsToMask int
	// FromEnd masks from the end instead of the start.
	FromEnd bool
}

func (m *Mask) Name() string { return "mask" }

func (m *Mask) Anonymize(text, _ string) (string, error) {
	ch := "*"
	if m.MaskingChar != "" {
		ch = string([]rune(m.MaskingChar)[0:1])
	}
	runes := []rune(text)
	n := len(runes)
	count := n
	if m.CharsToMask > 0 && m.CharsToMask < n {
		count = m.CharsToMask
	}
	result := make([]rune, n)
	copy(result, runes)
	if m.FromEnd {
		for i := n - count; i < n; i++ {
			result[i] = []rune(ch)[0]
		}
	} else {
		for i := 0; i < count; i++ {
			result[i] = []rune(ch)[0]
		}
	}
	return string(result), nil
}
