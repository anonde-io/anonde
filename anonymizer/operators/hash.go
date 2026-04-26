package operators

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
)

// HashType selects the hashing algorithm.
type HashType string

const (
	HashSHA256 HashType = "sha256"
	HashSHA512 HashType = "sha512"
)

// Hash replaces PII with its cryptographic hash.
type Hash struct {
	// HashType is the algorithm to use (default: sha256).
	HashType HashType
}

func (h *Hash) Name() string { return "hash" }

func (h *Hash) Anonymize(text, _ string) (string, error) {
	switch h.HashType {
	case HashSHA512:
		sum := sha512.Sum512([]byte(text))
		return hex.EncodeToString(sum[:]), nil
	case HashSHA256, "":
		sum := sha256.Sum256([]byte(text))
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("unsupported hash type: %s", h.HashType)
	}
}
