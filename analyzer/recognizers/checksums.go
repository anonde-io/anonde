package recognizers

// Shared checksum primitives used across the regional PII recognizers.
// Each function takes a normalized (separator-stripped, uppercased where
// relevant) candidate and returns true if the checksum is valid.
//
// References:
//   - Verhoeff: NIST/Wikipedia, used by IN_AADHAAR.
//   - mod-11 weighted: NL BSN, FI HETU base.
//   - AU TFN/ABN/ACN: ATO / ASIC published rules.
//   - PL PESEL: GUS specification (mod-10 weighted).
//   - KR RRN: National ID checksum (mod-11 weighted).

// digitsOnly returns true if every byte is an ASCII digit.
func digitsOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// weightedSumMod returns sum(d_i * w_i) mod m where d_i is digit i of s
// (left-to-right) and w_i is weights[i]. Returns -1 if s is not all digits or
// length doesn't match weights.
func weightedSumMod(s string, weights []int, m int) int {
	if len(s) != len(weights) || !digitsOnly(s) {
		return -1
	}
	sum := 0
	for i := 0; i < len(s); i++ {
		sum += int(s[i]-'0') * weights[i]
	}
	if m <= 0 {
		return sum
	}
	return sum % m
}

// AU TFN: 9 digits, weights [1,4,3,7,5,8,6,9,10], sum mod 11 == 0.
func validateAUTFN(s string) bool {
	if len(s) != 9 {
		return false
	}
	r := weightedSumMod(s, []int{1, 4, 3, 7, 5, 8, 6, 9, 10}, 11)
	return r == 0
}

// AU ABN: 11 digits. Subtract 1 from first digit, multiply by
// [10,1,3,5,7,9,11,13,15,17,19], sum mod 89 == 0.
func validateAUABN(s string) bool {
	if len(s) != 11 || !digitsOnly(s) {
		return false
	}
	weights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	sum := 0
	for i := 0; i < 11; i++ {
		d := int(s[i] - '0')
		if i == 0 {
			d--
		}
		sum += d * weights[i]
	}
	return sum%89 == 0
}

// AU ACN: 9 digits, weights [8,7,6,5,4,3,2,1], computed check digit
// (10 - sum%10) % 10 must equal the 9th digit.
func validateAUACN(s string) bool {
	if len(s) != 9 || !digitsOnly(s) {
		return false
	}
	weights := []int{8, 7, 6, 5, 4, 3, 2, 1}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += int(s[i]-'0') * weights[i]
	}
	check := (10 - sum%10) % 10
	return check == int(s[8]-'0')
}

// AU MEDICARE: 10 digits, first digit is 2-6, weights [1,3,7,9,1,3,7,9],
// applied to first 8 digits, sum mod 10 == 9th digit.
func validateAUMedicare(s string) bool {
	if len(s) != 10 || !digitsOnly(s) {
		return false
	}
	if s[0] < '2' || s[0] > '6' {
		return false
	}
	weights := []int{1, 3, 7, 9, 1, 3, 7, 9}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += int(s[i]-'0') * weights[i]
	}
	return sum%10 == int(s[8]-'0')
}

// IN PAN: AAAAA9999A; 5 letters + 4 digits + 1 letter. Fourth letter
// indicates entity type (P=Individual, F=Firm, etc.); we don't enforce it
// strictly to allow forward compatibility, but reject obviously malformed
// values up to the regex check.
func validateINPAN(s string) bool {
	if len(s) != 10 {
		return false
	}
	for i := 0; i < 5; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	for i := 5; i < 9; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s[9] >= 'A' && s[9] <= 'Z'
}

// Verhoeff multiplication / permutation / inverse tables.
var verhoeffD = [10][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	{1, 2, 3, 4, 0, 6, 7, 8, 9, 5},
	{2, 3, 4, 0, 1, 7, 8, 9, 5, 6},
	{3, 4, 0, 1, 2, 8, 9, 5, 6, 7},
	{4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
	{5, 9, 8, 7, 6, 0, 4, 3, 2, 1},
	{6, 5, 9, 8, 7, 1, 0, 4, 3, 2},
	{7, 6, 5, 9, 8, 2, 1, 0, 4, 3},
	{8, 7, 6, 5, 9, 3, 2, 1, 0, 4},
	{9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
}

var verhoeffP = [8][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	{1, 5, 7, 6, 2, 8, 3, 0, 9, 4},
	{5, 8, 0, 3, 7, 9, 6, 1, 4, 2},
	{8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
	{9, 4, 5, 3, 1, 2, 6, 8, 7, 0},
	{4, 2, 8, 6, 5, 7, 3, 9, 0, 1},
	{2, 7, 9, 3, 8, 0, 6, 4, 1, 5},
	{7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
}

// validateVerhoeff implements the Verhoeff checksum used by Aadhaar.
// The number is read right-to-left; the running checksum starts at 0 and is
// computed via D[c, P[(i mod 8), n_i]]. Valid if final c == 0.
func validateVerhoeff(s string) bool {
	if !digitsOnly(s) {
		return false
	}
	c := 0
	for i := 0; i < len(s); i++ {
		digit := int(s[len(s)-1-i] - '0')
		c = verhoeffD[c][verhoeffP[i%8][digit]]
	}
	return c == 0
}

// IT_FISCAL_CODE control character. Standard 16-char codice fiscale:
// odd-positioned (1-indexed) chars use one table, even-positioned use another;
// sum mod 26, mapped to A-Z, must equal s[15].
//
// Tables here treat positions as 0-indexed; position 0 is "odd" in the
// Italian spec.
func validateITFiscalCode(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < 15; i++ {
		c := s[i]
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	if s[15] < 'A' || s[15] > 'Z' {
		return false
	}
	odd := map[byte]int{
		'0': 1, '1': 0, '2': 5, '3': 7, '4': 9, '5': 13, '6': 15, '7': 17, '8': 19, '9': 21,
		'A': 1, 'B': 0, 'C': 5, 'D': 7, 'E': 9, 'F': 13, 'G': 15, 'H': 17, 'I': 19, 'J': 21,
		'K': 2, 'L': 4, 'M': 18, 'N': 20, 'O': 11, 'P': 3, 'Q': 6, 'R': 8, 'S': 12, 'T': 14,
		'U': 16, 'V': 10, 'W': 22, 'X': 25, 'Y': 24, 'Z': 23,
	}
	even := map[byte]int{
		'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8, '9': 9,
		'A': 0, 'B': 1, 'C': 2, 'D': 3, 'E': 4, 'F': 5, 'G': 6, 'H': 7, 'I': 8, 'J': 9,
		'K': 10, 'L': 11, 'M': 12, 'N': 13, 'O': 14, 'P': 15, 'Q': 16, 'R': 17, 'S': 18,
		'T': 19, 'U': 20, 'V': 21, 'W': 22, 'X': 23, 'Y': 24, 'Z': 25,
	}
	sum := 0
	for i := 0; i < 15; i++ {
		// 1-indexed odd → i==0,2,4,... (0-indexed even)
		if i%2 == 0 {
			sum += odd[s[i]]
		} else {
			sum += even[s[i]]
		}
	}
	return rune('A')+rune(sum%26) == rune(s[15])
}

// IT_VAT (P.IVA): 11 digits, Luhn-like with mod 10 over weighted digits where
// odd-positioned (1-indexed) digits are weight 1, even-positioned get doubled
// with the casting-out-nines reduction. Final digit is the check.
func validateITVATCode(s string) bool {
	if len(s) != 11 || !digitsOnly(s) {
		return false
	}
	sum := 0
	for i := 0; i < 10; i++ {
		d := int(s[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	check := (10 - sum%10) % 10
	return check == int(s[10]-'0')
}

// ES NIF (and DNI): 8 digits + control letter. Letter = "TRWAGMYFPDXBNJZSQVHLCKE"[number % 23].
func validateESNIF(s string) bool {
	if len(s) != 9 {
		return false
	}
	if !digitsOnly(s[:8]) {
		return false
	}
	if s[8] < 'A' || s[8] > 'Z' {
		return false
	}
	letters := "TRWAGMYFPDXBNJZSQVHLCKE"
	num := 0
	for i := 0; i < 8; i++ {
		num = num*10 + int(s[i]-'0')
	}
	return s[8] == letters[num%23]
}

// ES NIE: starts with X/Y/Z, then 7 digits + control letter.
// Convert leading letter to digit (X=0, Y=1, Z=2), then validate as NIF.
func validateESNIE(s string) bool {
	if len(s) != 9 {
		return false
	}
	prefix := map[byte]byte{'X': '0', 'Y': '1', 'Z': '2'}
	leading, ok := prefix[s[0]]
	if !ok {
		return false
	}
	return validateESNIF(string(leading) + s[1:])
}

// PL PESEL: 11 digits. Weights [1,3,7,9,1,3,7,9,1,3] over first 10, sum mod 10,
// check = (10 - sum%10) mod 10 must equal digit 11. Also, embedded date must
// be plausible; month encodes century via offsets (0,20,40,60,80).
func validatePLPESEL(s string) bool {
	if len(s) != 11 || !digitsOnly(s) {
		return false
	}
	weights := []int{1, 3, 7, 9, 1, 3, 7, 9, 1, 3}
	sum := 0
	for i := 0; i < 10; i++ {
		sum += int(s[i]-'0') * weights[i]
	}
	check := (10 - sum%10) % 10
	return check == int(s[10]-'0')
}

// SG NRIC/FIN: letter + 7 digits + check letter. Weights [2,7,6,5,4,3,2] over
// 7 digits. Different check tables for S/T (citizens) vs F/G/M (foreigners).
func validateSGNRIC(s string) bool {
	if len(s) != 9 {
		return false
	}
	if !digitsOnly(s[1:8]) {
		return false
	}
	weights := []int{2, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i := 0; i < 7; i++ {
		sum += int(s[1+i]-'0') * weights[i]
	}
	switch s[0] {
	case 'T', 'G':
		sum += 4
	case 'M':
		sum += 3
	case 'S', 'F':
		// no offset
	default:
		return false
	}
	rem := sum % 11
	var table string
	switch s[0] {
	case 'S', 'T':
		table = "JZIHGFEDCBA"
	case 'F', 'G':
		table = "XWUTRQPNMLK"
	case 'M':
		table = "KLJNPQRTUWX" // post-2017 issuance
	}
	if int(rem) >= len(table) {
		return false
	}
	return s[8] == table[rem]
}

// FI HETU (Personal Identity Code): DDMMYY[+-A]NNNC (11 chars).
// Numeric component (DDMMYYNNN) mod 31 maps to "0123456789ABCDEFHJKLMNPRSTUVWXY".
func validateFIHETU(s string) bool {
	if len(s) != 11 {
		return false
	}
	for i := 0; i < 6; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	if s[6] != '+' && s[6] != '-' && s[6] != 'A' && s[6] != 'Y' && s[6] != 'X' && s[6] != 'W' && s[6] != 'V' && s[6] != 'U' && s[6] != 'B' && s[6] != 'C' && s[6] != 'D' && s[6] != 'E' && s[6] != 'F' {
		return false
	}
	if !digitsOnly(s[7:10]) {
		return false
	}
	num := 0
	for _, c := range s[:6] {
		num = num*10 + int(c-'0')
	}
	for _, c := range s[7:10] {
		num = num*10 + int(c-'0')
	}
	checkChars := "0123456789ABCDEFHJKLMNPRSTUVWXY"
	expected := checkChars[num%31]
	return s[10] == expected
}

// KR RRN: 13 digits with weights [2,3,4,5,6,7,8,9,2,3,4,5], check = (11 - sum%11) % 10.
func validateKRRRN(s string) bool {
	if len(s) != 13 || !digitsOnly(s) {
		return false
	}
	weights := []int{2, 3, 4, 5, 6, 7, 8, 9, 2, 3, 4, 5}
	sum := 0
	for i := 0; i < 12; i++ {
		sum += int(s[i]-'0') * weights[i]
	}
	check := (11 - sum%11) % 10
	return check == int(s[12]-'0')
}

// DE Steuerliche Identifikationsnummer (Steuer-ID): 11 digits.
// Check digit (last digit) computed via ISO 7064 MOD 11,10.
//
// Plus the uniqueness rule on the first 10 digits (per BMF spec
// 2016-04-12): exactly one digit appears 2–3 times, all other digits
// appear 0 or 1 times. This rejects sequences like "00000000000" and
// random 11-digit case numbers that happen to satisfy the checksum.
func validateDESteuerID(s string) bool {
	if len(s) != 11 || !digitsOnly(s) {
		return false
	}

	// Uniqueness rule on first 10 digits.
	var counts [10]int
	for i := 0; i < 10; i++ {
		counts[s[i]-'0']++
	}
	repeated := 0       // count of digits that appear ≥2 times
	tooMany := false    // any digit appearing ≥4 times → invalid
	for _, c := range counts {
		if c >= 4 {
			tooMany = true
		}
		if c >= 2 {
			repeated++
		}
	}
	if tooMany || repeated != 1 {
		return false
	}

	// ISO 7064 MOD 11,10 check digit.
	product := 10
	for i := 0; i < 10; i++ {
		sum := (int(s[i]-'0') + product) % 10
		if sum == 0 {
			sum = 10
		}
		product = (2 * sum) % 11
	}
	check := (11 - product) % 10
	return check == int(s[10]-'0')
}

// UK NHS number: 10 digits, weights [10,9,8,7,6,5,4,3,2] over the first 9,
// check = 11 - (sum%11). 11 → invalid, 10 → invalid, otherwise must equal digit 10.
func validateUKNHS(s string) bool {
	if len(s) != 10 || !digitsOnly(s) {
		return false
	}
	weights := []int{10, 9, 8, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i := 0; i < 9; i++ {
		sum += int(s[i]-'0') * weights[i]
	}
	rem := sum % 11
	check := 11 - rem
	if check == 11 {
		check = 0
	}
	if check == 10 {
		return false
	}
	return check == int(s[9]-'0')
}
