package recognizers

import "regexp"

var (
	// Bitcoin legacy (P2PKH/P2SH) and bech32 addresses.
	btcRE = regexp.MustCompile(`\b(?:[13][a-km-zA-HJ-NP-Z1-9]{25,34}|bc1[ac-hj-np-z02-9]{6,87})\b`)
	// Ethereum addresses.
	ethRE = regexp.MustCompile(`\b0x[0-9a-fA-F]{40}\b`)
)

// NewCryptoRecognizer detects CRYPTO (Bitcoin/Ethereum) wallet addresses.
func NewCryptoRecognizer() *PatternRecognizer {
	return NewPatternRecognizer(
		"CryptoRecognizer",
		[]string{"CRYPTO"},
		[]string{"*"},
		[]namedPattern{
			{re: btcRE, score: 0.9},
			{re: ethRE, score: 0.9},
		},
	)
}
