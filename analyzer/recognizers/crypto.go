package recognizers

import (
	"context"
	"crypto/sha256"
	"regexp"

	"github.com/anonde-io/anonde/analyzer"
)

var (
	// Bitcoin legacy (P2PKH/P2SH) addresses — base58, 25–34 chars, leading "1" or "3".
	btcRE = regexp.MustCompile(`\b[13][a-km-zA-HJ-NP-Z1-9]{25,34}\b`)
	// Bitcoin bech32 (segwit) — leading "bc1" then 6–87 lowercase base32 chars.
	bech32RE = regexp.MustCompile(`\bbc1[ac-hj-np-z02-9]{6,87}\b`)
	// Ethereum addresses — fixed-length hex.
	ethRE = regexp.MustCompile(`\b0x[0-9a-fA-F]{40}\b`)
)

// CryptoRecognizer detects cryptocurrency wallet addresses with format-aware
// score adjustment:
//
//   - Ethereum 0x-addresses: pure-format match, no checksum (EIP-55 mixed-case
//     checksumming is non-standard for many real-world emissions). Score 0.9.
//   - Bitcoin bech32: leading "bc1" + base32 charset. Score 0.85 (no checksum
//     verification yet — pull request welcome).
//   - Bitcoin legacy (P2PKH/P2SH): Base58Check validated. Score 1.0 on pass,
//     finding dropped on fail. This eliminates the bulk of false positives —
//     the 25–34 base58 character pattern is otherwise loose enough to match
//     random hashes / IDs that aren't real Bitcoin addresses.
type CryptoRecognizer struct{}

func NewCryptoRecognizer() *CryptoRecognizer { return &CryptoRecognizer{} }

func (c *CryptoRecognizer) Name() string                 { return "CryptoRecognizer" }
func (c *CryptoRecognizer) SupportedEntities() []string  { return []string{"CRYPTO"} }
func (c *CryptoRecognizer) SupportedLanguages() []string { return []string{"*"} }

// ContextKeywords implements analyzer.ContextProvider.
func (c *CryptoRecognizer) ContextKeywords() map[string][]string {
	return map[string][]string{
		"CRYPTO": {"wallet", "btc", "bitcoin", "eth", "ethereum", "crypto", "address"},
	}
}

func (c *CryptoRecognizer) Analyze(_ context.Context, text string, _ []string, _ string) ([]analyzer.RecognizerResult, error) {
	var results []analyzer.RecognizerResult

	for _, m := range ethRE.FindAllStringIndex(text, -1) {
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.9,
			EntityType: "CRYPTO", RecognizerName: "CryptoRecognizer",
		})
	}
	for _, m := range bech32RE.FindAllStringIndex(text, -1) {
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 0.85,
			EntityType: "CRYPTO", RecognizerName: "CryptoRecognizer",
		})
	}
	for _, m := range btcRE.FindAllStringIndex(text, -1) {
		candidate := text[m[0]:m[1]]
		if !validateBase58Check(candidate) {
			continue
		}
		results = append(results, analyzer.RecognizerResult{
			Start: m[0], End: m[1], Score: 1.0,
			EntityType: "CRYPTO", RecognizerName: "CryptoRecognizer",
		})
	}
	return results, nil
}

// validateBase58Check returns true if `s` is a valid base58-encoded payload
// whose final 4 bytes equal SHA-256(SHA-256(prefix)) — Bitcoin's legacy
// address checksum. Used to reject random base58-shaped strings that aren't
// real wallet addresses.
func validateBase58Check(s string) bool {
	decoded, ok := base58Decode(s)
	if !ok || len(decoded) < 5 {
		return false
	}
	payload := decoded[:len(decoded)-4]
	check := decoded[len(decoded)-4:]
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	return string(check) == string(second[:4])
}

// base58 alphabet (Bitcoin variant — no 0/O/I/l).
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58Index [128]int8

func init() {
	for i := range base58Index {
		base58Index[i] = -1
	}
	for i := 0; i < len(base58Alphabet); i++ {
		base58Index[base58Alphabet[i]] = int8(i)
	}
}

// base58Decode decodes Bitcoin-style base58 into bytes. Returns (nil, false)
// for any input outside the alphabet.
//
// Standard formulation: count leading '1's (representing leading zero
// bytes), then accumulate the remaining digits as a base-58 big integer
// and convert to base-256.
func base58Decode(s string) ([]byte, bool) {
	if len(s) == 0 {
		return nil, false
	}
	leadingZeros := 0
	for leadingZeros < len(s) && s[leadingZeros] == '1' {
		leadingZeros++
	}
	// Big-endian base-256 accumulator. Allocate generously: a base-58
	// digit contributes ~log256(58) ≈ 0.733 bytes per char.
	size := (len(s)-leadingZeros)*733/1000 + 1
	buf := make([]byte, size)
	for i := leadingZeros; i < len(s); i++ {
		c := s[i]
		if c >= 128 || base58Index[c] < 0 {
			return nil, false
		}
		carry := int(base58Index[c])
		for j := size - 1; j >= 0; j-- {
			carry += 58 * int(buf[j])
			buf[j] = byte(carry % 256)
			carry /= 256
		}
		if carry != 0 {
			return nil, false
		}
	}
	// Strip leading bytes that ended up zero (unused capacity).
	first := 0
	for first < len(buf) && buf[first] == 0 {
		first++
	}
	out := make([]byte, leadingZeros+(len(buf)-first))
	// Each leading '1' encodes one leading zero byte.
	for i := 0; i < leadingZeros; i++ {
		out[i] = 0
	}
	copy(out[leadingZeros:], buf[first:])
	return out, true
}
