package operators

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
)

// Synthesize replaces PII with realistic synthetic data of the same entity type,
// preserving format and structure (Luhn-valid credit cards, MOD-97 IBANs,
// class-preserving IP addresses, etc.). Unlike Redact or Replace, the resulting
// document stays readable and structurally intact — useful for test-data
// generation, LLM fine-tuning datasets, and privacy-safe log sharing.
//
// Consistency modes:
//
//   - Default (Consistent=false): a fresh random fake on every call.
//
//   - Consistent=true: the same (text, entityType) pair always produces the
//     same synthetic value. Seeded deterministically from the input — consistent
//     across processes and restarts. "John Smith" is always the same alias.
//
//   - Consistent=true + DocumentScoped=true: an in-memory cache maps
//     original→fake within this operator instance. Call Reset() between
//     documents. Different Synthesize instances produce independent mappings for
//     the same input, so "John Smith" in doc A and doc B get different aliases —
//     limiting cross-document linkability while remaining consistent within a doc.
type Synthesize struct {
	Consistent     bool
	DocumentScoped bool

	mu    sync.Mutex
	cache map[string]string
}

func (s *Synthesize) Name() string { return "synthesize" }

// Anonymize returns a plausible synthetic replacement for the given PII value.
func (s *Synthesize) Anonymize(text, entityType string) (string, error) {
	if !s.Consistent {
		return synthesizeEntity(cryptoRNG(), text, entityType)
	}
	if s.DocumentScoped {
		return s.fromCache(text, entityType)
	}
	return synthesizeEntity(hashRNG(entityType, text), text, entityType)
}

// Reset clears the document-scoped cache. Call between documents when
// DocumentScoped=true to prevent cross-document alias reuse.
func (s *Synthesize) Reset() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

// fromCache looks up or generates a document-scoped synthetic value.
// The first encounter for a (text, entityType) pair generates a random fake and
// caches it; subsequent calls within the same instance return the cached value.
func (s *Synthesize) fromCache(text, entityType string) (string, error) {
	key := entityType + "\x00" + text

	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]string)
	}
	if v, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()

	v, err := synthesizeEntity(cryptoRNG(), text, entityType)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	// Re-check the map: between the first unlock and this re-lock,
	// Reset() may have raced in and set s.cache = nil. Without this
	// guard the next assignment panics on a nil map. Do NOT remove —
	// the Reset() race is a real, documented contract.
	if s.cache == nil {
		s.cache = make(map[string]string)
	}
	if s.cache[key] == "" { // guard against a parallel writer
		s.cache[key] = v
	} else {
		v = s.cache[key]
	}
	s.mu.Unlock()
	return v, nil
}

// hashRNG returns a deterministic PRNG seeded from entityType and text.
func hashRNG(entityType, text string) *rand.Rand {
	h := sha256.Sum256([]byte(entityType + "\x00" + text))
	seed := int64(binary.LittleEndian.Uint64(h[:8]))
	return rand.New(rand.NewSource(seed)) //nolint:gosec
}

// cryptoRNG returns a PRNG seeded from crypto/rand.
func cryptoRNG() *rand.Rand {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return rand.New(rand.NewSource(0)) //nolint:gosec
	}
	return rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(b[:])))) //nolint:gosec
}

func synPick(rng *rand.Rand, list []string) string { return list[rng.Intn(len(list))] }

// synthesizeEntity dispatches to a per-type generator.
func synthesizeEntity(rng *rand.Rand, text, entityType string) (string, error) {
	switch entityType {
	case "PERSON":
		return synPick(rng, synFirstNames) + " " + synPick(rng, synLastNames), nil
	case "EMAIL_ADDRESS":
		fn := strings.ToLower(synPick(rng, synFirstNames))
		ln := strings.ToLower(synPick(rng, synLastNames))
		return fn + "." + ln + "@" + synPick(rng, synEmailDomains), nil
	case "PHONE_NUMBER":
		return synDigitPreserve(rng, text), nil
	case "CREDIT_CARD":
		return synCreditCard(rng, text), nil
	case "US_SSN":
		return synSSN(rng), nil
	case "IBAN_CODE":
		return synIBAN(rng, text), nil
	case "IP_ADDRESS":
		return synIP(rng, text), nil
	case "MAC_ADDRESS":
		return synMAC(rng, text), nil
	case "URL":
		return synURL(rng, text), nil
	case "DATE_TIME":
		return synDigitPreserve(rng, text), nil
	case "LOCATION":
		return synPick(rng, synCities), nil
	case "ORGANIZATION":
		return synPick(rng, synCompanies), nil
	case "CRYPTO":
		return synCrypto(rng, text), nil
	case "US_PASSPORT":
		return fmt.Sprintf("%c%08d", rune('A'+rng.Intn(26)), rng.Intn(100_000_000)), nil
	case "US_ITIN":
		return synUSITIN(rng), nil
	default:
		// Preserve format for any unrecognised type (US_BANK_NUMBER, driver licences, etc.)
		return synDigitPreserve(rng, text), nil
	}
}

// synDigitPreserve randomizes digits while keeping the format skeleton intact
// (dashes, spaces, parentheses, letters). Safe fallback for any structured type.
func synDigitPreserve(rng *rand.Rand, text string) string {
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if r >= '0' && r <= '9' {
			out = append(out, rune('0'+rng.Intn(10)))
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

// ── Credit card ───────────────────────────────────────────────────────────────

func synCreditCard(rng *rand.Rand, original string) string {
	// Collect digit count and separator positions (afterDigitIndex → chars).
	type sepEntry struct {
		afterIdx int
		ch       rune
	}
	var digitCount int
	var seps []sepEntry
	for _, r := range original {
		if r >= '0' && r <= '9' {
			digitCount++
		} else if digitCount > 0 {
			seps = append(seps, sepEntry{digitCount - 1, r})
		}
	}
	n := digitCount
	if n < 13 || n > 19 {
		return synDigitPreserve(rng, original)
	}

	// Generate n digits: first from a realistic BIN range, n-1 random, last = Luhn check.
	bins := []byte{'4', '5', '3', '6'}
	digits := make([]byte, n)
	digits[0] = bins[rng.Intn(len(bins))]
	for i := 1; i < n-1; i++ {
		digits[i] = byte('0' + rng.Intn(10))
	}
	digits[n-1] = synLuhnCheck(digits[:n-1])

	// Rebuild with original separators.
	sepMap := make(map[int][]rune)
	for _, s := range seps {
		sepMap[s.afterIdx] = append(sepMap[s.afterIdx], s.ch)
	}
	var b strings.Builder
	for i, d := range digits {
		b.WriteByte(d)
		for _, sep := range sepMap[i] {
			b.WriteRune(sep)
		}
	}
	return b.String()
}

// synLuhnCheck computes the Luhn check digit for a prefix of digits.
// Uses the same parity convention as the luhn validator in credit_card.go:
// parity = (total digits including check) % 2.
func synLuhnCheck(prefix []byte) byte {
	n := len(prefix) + 1 // total length including the check digit
	parity := n % 2
	sum := 0
	for i, d := range prefix {
		v := int(d - '0')
		if i%2 == parity {
			v *= 2
			if v > 9 {
				v -= 9
			}
		}
		sum += v
	}
	return byte('0' + (10-sum%10)%10)
}

// ── SSN ───────────────────────────────────────────────────────────────────────

func synSSN(rng *rand.Rand) string {
	// Area: 001–899, excluding 000 and 666.
	area := 1 + rng.Intn(898)
	if area == 666 {
		area = 667
	}
	group := 1 + rng.Intn(99)
	serial := 1 + rng.Intn(9999)
	return fmt.Sprintf("%03d-%02d-%04d", area, group, serial)
}

// ── IBAN ──────────────────────────────────────────────────────────────────────

func synIBAN(rng *rand.Rand, original string) string {
	norm := strings.ToUpper(strings.ReplaceAll(original, " ", ""))
	if len(norm) < 6 || !synIsAlpha(rune(norm[0])) || !synIsAlpha(rune(norm[1])) {
		return synDigitPreserve(rng, original)
	}
	cc := norm[:2]
	bbanLen := len(norm) - 4 // IBAN = CC(2) + check(2) + BBAN
	if bbanLen < 1 {
		return synDigitPreserve(rng, original)
	}

	// Generate an all-digit BBAN of the same length (covers the majority of countries).
	bban := make([]byte, bbanLen)
	for i := range bban {
		bban[i] = byte('0' + rng.Intn(10))
	}
	check := synIBANCheck(cc, string(bban))
	result := fmt.Sprintf("%s%02d%s", cc, check, string(bban))

	// Preserve space grouping (printed IBAN format: groups of 4).
	if strings.Contains(original, " ") {
		var groups []string
		for i := 0; i < len(result); i += 4 {
			end := i + 4
			if end > len(result) {
				end = len(result)
			}
			groups = append(groups, result[i:end])
		}
		return strings.Join(groups, " ")
	}
	return result
}

// synIBANCheck computes the 2-digit MOD-97 check number for a given country
// code and BBAN, using the standard rearrangement algorithm.
func synIBANCheck(cc, bban string) int {
	// Rearrange: BBAN + CC + "00", then replace A–Z with 10–35.
	arranged := bban + cc + "00"
	var num strings.Builder
	for _, r := range strings.ToUpper(arranged) {
		if r >= 'A' && r <= 'Z' {
			fmt.Fprintf(&num, "%d", int(r-'A'+10))
		} else {
			num.WriteRune(r)
		}
	}
	// Compute mod 97 incrementally to avoid big.Int.
	remainder := 0
	for _, ch := range num.String() {
		remainder = (remainder*10 + int(ch-'0')) % 97
	}
	check := 98 - remainder
	if check < 1 {
		check = 1
	}
	return check
}

func synIsAlpha(r rune) bool { return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') }

// ── IP address ────────────────────────────────────────────────────────────────

func synIP(rng *rand.Rand, original string) string {
	ip := net.ParseIP(strings.TrimSpace(original))
	if ip == nil {
		return synDigitPreserve(rng, original)
	}
	if ip4 := ip.To4(); ip4 != nil {
		if synIsPrivate4(ip4) {
			return synFakePrivate4(rng, ip4)
		}
		return synFakePublic4(rng)
	}
	// IPv6: randomise all bytes.
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return net.IP(b).String()
}

func synIsPrivate4(ip net.IP) bool {
	return ip[0] == 10 ||
		ip[0] == 127 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168)
}

func synFakePrivate4(rng *rand.Rand, orig net.IP) string {
	switch {
	case orig[0] == 10:
		return fmt.Sprintf("10.%d.%d.%d", rng.Intn(256), rng.Intn(256), 1+rng.Intn(254))
	case orig[0] == 172:
		return fmt.Sprintf("172.%d.%d.%d", 16+rng.Intn(16), rng.Intn(256), 1+rng.Intn(254))
	default:
		return fmt.Sprintf("192.168.%d.%d", rng.Intn(256), 1+rng.Intn(254))
	}
}

func synFakePublic4(rng *rand.Rand) string {
	for {
		ip := net.IP{
			byte(1 + rng.Intn(223)),
			byte(rng.Intn(256)),
			byte(rng.Intn(256)),
			byte(1 + rng.Intn(254)),
		}
		if !synIsPrivate4(ip) {
			return ip.String()
		}
	}
}

// ── MAC address ───────────────────────────────────────────────────────────────

func synMAC(rng *rand.Rand, original string) string {
	sep := ":"
	if strings.Contains(original, "-") {
		sep = "-"
	}
	upper := original != strings.ToLower(original)

	b := make([]byte, 6)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	// Set locally-administered bit to avoid colliding with real OUIs.
	b[0] = (b[0] & 0xFE) | 0x02 // locally administered, unicast

	octets := make([]string, 6)
	for i, v := range b {
		if upper {
			octets[i] = fmt.Sprintf("%02X", v)
		} else {
			octets[i] = fmt.Sprintf("%02x", v)
		}
	}
	return strings.Join(octets, sep)
}

// ── URL ───────────────────────────────────────────────────────────────────────

func synURL(rng *rand.Rand, original string) string {
	scheme := "https"
	if strings.HasPrefix(strings.ToLower(original), "http://") {
		scheme = "http"
	}
	name := strings.ToLower(synPick(rng, synFirstNames)) + strings.ToLower(synPick(rng, synLastNames))
	tld := synPick(rng, synTLDs)
	return scheme + "://" + name + tld
}

// ── Crypto wallets ────────────────────────────────────────────────────────────

func synCrypto(rng *rand.Rand, original string) string {
	lower := strings.ToLower(original)

	// Ethereum: 0x + 40 hex chars.
	if strings.HasPrefix(lower, "0x") && len(original) == 42 {
		const hexChars = "0123456789abcdef"
		out := make([]byte, 42)
		copy(out[:2], "0x")
		for i := 2; i < 42; i++ {
			out[i] = hexChars[rng.Intn(16)]
		}
		return string(out)
	}

	// Bech32 Bitcoin (bc1…).
	if strings.HasPrefix(lower, "bc1") {
		const bech32 = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
		n := len(original)
		out := make([]byte, n)
		copy(out[:3], "bc1")
		for i := 3; i < n; i++ {
			out[i] = bech32[rng.Intn(len(bech32))]
		}
		return string(out)
	}

	// Bitcoin legacy (starts with 1 or 3), base58.
	const base58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := len(original)
	if n < 26 || n > 35 {
		n = 34
	}
	out := make([]byte, n)
	out[0] = '1'
	for i := 1; i < n; i++ {
		out[i] = base58[rng.Intn(len(base58))]
	}
	return string(out)
}

// ── US ITIN ───────────────────────────────────────────────────────────────────

func synUSITIN(rng *rand.Rand) string {
	// ITIN: 9NN-GG-SSSS. Area always 9xx; group in IRS-assigned ranges.
	validGroups := []int{
		70, 71, 72, 73, 74, 75, 76, 77, 78, 79,
		80, 81, 82, 83, 84, 85, 86, 87, 88,
		90, 91, 92, 94, 95, 96, 97, 98, 99,
	}
	area := 900 + rng.Intn(100)
	group := validGroups[rng.Intn(len(validGroups))]
	serial := 1 + rng.Intn(9999)
	return fmt.Sprintf("%d-%02d-%04d", area, group, serial)
}

// ── Embedded word lists ───────────────────────────────────────────────────────

var synFirstNames = []string{
	"Alice", "Benjamin", "Carmen", "David", "Elena", "Franklin", "Grace", "Hiroshi",
	"Ingrid", "James", "Kavya", "Liam", "Maria", "Noah", "Olivia", "Patrick",
	"Quinn", "Rafael", "Sophia", "Thomas", "Uma", "Victor", "Wendy", "Xander",
	"Yara", "Zachary", "Amara", "Brendan", "Chloe", "Diego", "Emeka", "Fatima",
	"Gabriel", "Hannah", "Ivan", "Julia", "Kenji", "Laura", "Marcus", "Nadia",
	"Oscar", "Priya", "Riley", "Samuel", "Talia", "Usman", "Valeria", "William",
}

var synLastNames = []string{
	"Anderson", "Bhattacharya", "Chen", "Diallo", "Evans", "Fischer", "Garcia",
	"Hassan", "Ivanova", "Johnson", "Kim", "Lopez", "Martinez", "Nakamura",
	"Okonkwo", "Patel", "Quinn", "Rodriguez", "Singh", "Thompson", "Ueda",
	"Vasquez", "Williams", "Yamamoto", "Zhang", "Adeyemi", "Brown", "Carvalho",
	"Dubois", "Eriksson", "Fontaine", "Gonzalez", "Huang", "Ibrahim", "Jensen",
	"Kowalski", "Leblanc", "Nguyen", "Olawale", "Park", "Reyes", "Santos",
	"Takahashi", "Ullah", "Volkov", "Walker", "Young",
}

var synEmailDomains = []string{
	"gmail.com", "yahoo.com", "outlook.com", "protonmail.com", "icloud.com",
	"hotmail.com", "fastmail.com", "tutanota.com", "zoho.com", "mail.com",
}

var synCities = []string{
	"Amsterdam", "Austin", "Bangkok", "Barcelona", "Berlin", "Brussels",
	"Buenos Aires", "Cairo", "Cape Town", "Chicago", "Copenhagen", "Dubai",
	"Dublin", "Edinburgh", "Frankfurt", "Geneva", "Helsinki", "Istanbul",
	"Jakarta", "Johannesburg", "Kyoto", "Lagos", "Lisbon", "London",
	"Lyon", "Madrid", "Melbourne", "Mexico City", "Milan", "Montreal",
	"Mumbai", "Munich", "Nairobi", "New York", "Oslo", "Paris",
	"Portland", "Prague", "Santiago", "São Paulo", "Seattle", "Seoul",
	"Singapore", "Stockholm", "Sydney", "Tokyo", "Toronto", "Vienna",
	"Warsaw", "Zurich",
}

var synCompanies = []string{
	"Apex Solutions", "Bridgewater Analytics", "Cascade Technologies",
	"Driftwood Consulting", "Evergreen Systems", "Falcon Digital",
	"Greenfield Ventures", "Harbor Capital", "Ironclad Labs",
	"Juniper Networks Inc", "Keystone Partners", "Lakeside Software",
	"Meridian Group", "Northstar Dynamics", "Onyx Innovations",
	"Pinnacle Services", "Quantum Leap Corp", "Redwood Strategies",
	"Silverstream Media", "Thorngate Solutions", "Unified Analytics",
	"Vanguard Systems", "Whitmore Associates", "Xenon Technologies",
	"Yellowstone Digital", "Zenith Consulting",
}

var synTLDs = []string{
	".com", ".io", ".net", ".org", ".co", ".app", ".dev", ".tech",
}
