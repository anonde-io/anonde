package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// AES-256-GCM envelope for vault values.
//
// Why GCM specifically: authenticated encryption (AEAD) — we get
// confidentiality + integrity in one primitive, so a tampered ciphertext
// fails the Open check instead of silently producing garbage cleartext
// the analyzer would then operate on.
//
// Wire layout per value: [12-byte random nonce][AES-GCM ciphertext+tag].
// The nonce is per-value (not per-deploy), so re-encrypting the same
// cleartext at the same tenant/token coordinates produces different
// bytes on disk — no info leak from repeated-value comparison.
//
// Key source: ANONDE_VAULT_KEY env var, base64-encoded 32 bytes. We
// fail loud at startup if it isn't set when bbolt is the backend.
// Upgrade path: replace LoadVaultKey with a KMS-backed key fetch
// (AWS KMS / GCP KMS / Vault), keeping the AEAD wire format unchanged.

const vaultKeySize = 32 // AES-256

// ErrVaultKeyMissing means the env var isn't set; callers should
// surface this at startup, not on the first request.
var ErrVaultKeyMissing = errors.New("ANONDE_VAULT_KEY not set")

// LoadVaultKey reads the encryption key from ANONDE_VAULT_KEY. Returns
// a 32-byte slice or an error. The env var must be base64-encoded.
//
// To generate a fresh key:
//
//	head -c 32 /dev/urandom | base64
func LoadVaultKey() ([]byte, error) {
	raw := os.Getenv("ANONDE_VAULT_KEY")
	if raw == "" {
		return nil, ErrVaultKeyMissing
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("ANONDE_VAULT_KEY: invalid base64: %w", err)
	}
	if len(key) != vaultKeySize {
		return nil, fmt.Errorf("ANONDE_VAULT_KEY: want %d bytes, got %d", vaultKeySize, len(key))
	}
	return key, nil
}

// aeadCipher wraps an AEAD with the constant nonce size for sealing.
// Kept private; constructed once at startup and passed to BoltVault.
type aeadCipher struct {
	aead cipher.AEAD
}

func newAEAD(key []byte) (*aeadCipher, error) {
	if len(key) != vaultKeySize {
		return nil, fmt.Errorf("aead key: want %d bytes, got %d", vaultKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &aeadCipher{aead: aead}, nil
}

// seal encrypts plaintext and returns nonce||ciphertext (caller stores
// it as a single opaque blob; no separate nonce table needed).
func (c *aeadCipher) seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	// Seal appends ciphertext+tag onto the dst slice; pre-allocating
	// `nonce` and growing it gives us one allocation for the result.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// open splits nonce||ciphertext, authenticates and decrypts.
// Tampered or truncated blobs return an error — there's no partial
// success path.
func (c *aeadCipher) open(blob []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(blob) < nonceSize {
		return nil, errors.New("vault blob too short")
	}
	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	return c.aead.Open(nil, nonce, ciphertext, nil)
}
