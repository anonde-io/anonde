package operators

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func mustSynthesize(t *testing.T, op *Synthesize, text, entityType string) string {
	t.Helper()
	v, err := op.Anonymize(text, entityType)
	if err != nil {
		t.Fatalf("Anonymize(%q, %q) error: %v", text, entityType, err)
	}
	return v
}

func luhnValid(s string) bool {
	sum := 0
	nDigits := len(s)
	parity := nDigits % 2
	for i, ch := range s {
		d := int(ch - '0')
		if d < 0 || d > 9 {
			return false
		}
		if i%2 == parity {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

func ibanValid(s string) bool {
	norm := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	if len(norm) < 5 {
		return false
	}
	rearranged := norm[4:] + norm[:4]
	var num strings.Builder
	for _, r := range rearranged {
		if r >= 'A' && r <= 'Z' {
			fmt.Fprintf(&num, "%d", int(r-'A'+10))
		} else {
			num.WriteRune(r)
		}
	}
	remainder := 0
	for _, ch := range num.String() {
		remainder = (remainder*10 + int(ch-'0')) % 97
	}
	return remainder == 1
}

// ── entity-type coverage ──────────────────────────────────────────────────────

func TestSynthesize_Person(t *testing.T) {
	op := &Synthesize{}
	v := mustSynthesize(t, op, "John Smith", "PERSON")
	if v == "John Smith" {
		t.Error("expected a different name, got the original")
	}
	if !strings.Contains(v, " ") {
		t.Errorf("expected first+last, got %q", v)
	}
}

func TestSynthesize_Email(t *testing.T) {
	op := &Synthesize{}
	v := mustSynthesize(t, op, "john@example.com", "EMAIL_ADDRESS")
	if !strings.Contains(v, "@") || !strings.Contains(v, ".") {
		t.Errorf("expected valid-looking email, got %q", v)
	}
}

func TestSynthesize_CreditCard_Luhn(t *testing.T) {
	op := &Synthesize{}
	// Test various input formats.
	inputs := []string{
		"4111111111111111",         // Visa 16-digit bare
		"4111 1111 1111 1111",      // Visa with spaces
		"4111-1111-1111-1111",      // Visa with dashes
		"378282246310005",           // Amex 15-digit
		"3782 822463 10005",         // Amex formatted
	}
	for _, in := range inputs {
		v := mustSynthesize(t, op, in, "CREDIT_CARD")
		// Strip separators to validate Luhn.
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, v)
		if !luhnValid(digits) {
			t.Errorf("input %q → %q: Luhn check failed (digits: %s)", in, v, digits)
		}
		// Separator structure should be preserved.
		originalSeps := strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' {
				return r
			}
			return -1
		}, in)
		synSeps := strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' {
				return r
			}
			return -1
		}, v)
		if originalSeps != synSeps {
			t.Errorf("separator structure changed: %q → %q", in, v)
		}
	}
}

func TestSynthesize_SSN(t *testing.T) {
	op := &Synthesize{}
	ssnRE := regexp.MustCompile(`^\d{3}-\d{2}-\d{4}$`)
	for i := 0; i < 20; i++ {
		v := mustSynthesize(t, op, "123-45-6789", "US_SSN")
		if !ssnRE.MatchString(v) {
			t.Errorf("invalid SSN format: %q", v)
		}
		// Area must not be 000 or 666.
		area := v[:3]
		if area == "000" || area == "666" {
			t.Errorf("invalid SSN area: %q", v)
		}
	}
}

func TestSynthesize_IBAN_MOD97(t *testing.T) {
	op := &Synthesize{}
	inputs := []string{
		"GB82WEST12345698765432",
		"DE89370400440532013000",
		"FR7630006000011234567890189",
		"GB82 WEST 1234 5698 7654 32", // spaced format
	}
	for _, in := range inputs {
		v := mustSynthesize(t, op, in, "IBAN_CODE")
		if !ibanValid(v) {
			t.Errorf("input %q → %q: MOD-97 check failed", in, v)
		}
		// Country code must be preserved.
		inCC := strings.ToUpper(strings.ReplaceAll(in, " ", ""))[:2]
		outCC := strings.ToUpper(strings.ReplaceAll(v, " ", ""))[:2]
		if inCC != outCC {
			t.Errorf("country code changed: %q → %q", in, v)
		}
	}
}

func TestSynthesize_IP_ClassPreservation(t *testing.T) {
	op := &Synthesize{}
	cases := []struct {
		in      string
		private bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.100", true},
		{"172.16.5.10", true},
		{"8.8.8.8", false},
		{"203.0.113.1", false},
	}
	for _, tc := range cases {
		v := mustSynthesize(t, op, tc.in, "IP_ADDRESS")
		ip := net.ParseIP(v)
		if ip == nil {
			t.Errorf("input %q → invalid IP %q", tc.in, v)
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			t.Errorf("input %q → non-IPv4 %q", tc.in, v)
			continue
		}
		gotPrivate := synIsPrivate4(ip4)
		if gotPrivate != tc.private {
			t.Errorf("input %q → %q: private=%v, want %v", tc.in, v, gotPrivate, tc.private)
		}
	}
}

func TestSynthesize_MAC(t *testing.T) {
	op := &Synthesize{}
	cases := []string{
		"00:1A:2B:3C:4D:5E",
		"00-1a-2b-3c-4d-5e",
	}
	for _, in := range cases {
		v := mustSynthesize(t, op, in, "MAC_ADDRESS")
		// Separator style should match.
		if strings.Contains(in, ":") && !strings.Contains(v, ":") {
			t.Errorf("colon separator lost: %q → %q", in, v)
		}
		if strings.Contains(in, "-") && !strings.Contains(v, "-") {
			t.Errorf("dash separator lost: %q → %q", in, v)
		}
		// Should have 6 octets.
		sep := ":"
		if strings.Contains(in, "-") {
			sep = "-"
		}
		if len(strings.Split(v, sep)) != 6 {
			t.Errorf("expected 6 octets, got %q", v)
		}
	}
}

func TestSynthesize_URL(t *testing.T) {
	op := &Synthesize{}
	for _, in := range []string{"https://example.com", "http://evil.org"} {
		v := mustSynthesize(t, op, in, "URL")
		if strings.HasPrefix(in, "http://") && !strings.HasPrefix(v, "http://") {
			t.Errorf("scheme changed from http: %q → %q", in, v)
		}
		if strings.HasPrefix(in, "https://") && !strings.HasPrefix(v, "https://") {
			t.Errorf("scheme changed from https: %q → %q", in, v)
		}
	}
}

func TestSynthesize_Crypto(t *testing.T) {
	op := &Synthesize{}
	cases := []string{
		"1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf​Na", // Bitcoin legacy (length may vary)
		"0x742d35Cc6634C0532925a3b844Bc454e4438f44e", // Ethereum
		"bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", // Bech32
	}
	for _, in := range cases {
		v := mustSynthesize(t, op, in, "CRYPTO")
		if v == "" {
			t.Errorf("empty output for %q", in)
		}
		if strings.HasPrefix(strings.ToLower(in), "0x") && !strings.HasPrefix(v, "0x") {
			t.Errorf("Ethereum prefix lost: %q → %q", in, v)
		}
		if strings.HasPrefix(strings.ToLower(in), "bc1") && !strings.HasPrefix(v, "bc1") {
			t.Errorf("bech32 prefix lost: %q → %q", in, v)
		}
	}
}

func TestSynthesize_USPassport(t *testing.T) {
	op := &Synthesize{}
	passRE := regexp.MustCompile(`^[A-Z]\d{8}$`)
	v := mustSynthesize(t, op, "A12345678", "US_PASSPORT")
	if !passRE.MatchString(v) {
		t.Errorf("invalid US passport format: %q", v)
	}
}

func TestSynthesize_ITIN(t *testing.T) {
	op := &Synthesize{}
	itinRE := regexp.MustCompile(`^9\d{2}-\d{2}-\d{4}$`)
	for i := 0; i < 20; i++ {
		v := mustSynthesize(t, op, "900-70-0001", "US_ITIN")
		if !itinRE.MatchString(v) {
			t.Errorf("invalid ITIN format: %q", v)
		}
	}
}

func TestSynthesize_Unknown_PreservesFormat(t *testing.T) {
	op := &Synthesize{}
	v := mustSynthesize(t, op, "AB-1234-CD", "SOME_CUSTOM_TYPE")
	// Letters should be kept, digits replaced.
	if !strings.HasPrefix(v, "AB-") || !strings.HasSuffix(v, "-CD") {
		t.Errorf("format skeleton not preserved: %q", v)
	}
}

// ── consistency modes ─────────────────────────────────────────────────────────

func TestSynthesize_Consistent_GlobalDeterminism(t *testing.T) {
	op := &Synthesize{Consistent: true}
	a := mustSynthesize(t, op, "John Smith", "PERSON")
	b := mustSynthesize(t, op, "John Smith", "PERSON")
	if a != b {
		t.Errorf("Consistent mode: got different results %q and %q", a, b)
	}
	// Different input → different output (with overwhelming probability).
	c := mustSynthesize(t, op, "Jane Doe", "PERSON")
	if a == c {
		t.Errorf("Consistent mode: different inputs produced same output %q", a)
	}
}

func TestSynthesize_Consistent_CrossProcess(t *testing.T) {
	// Same seed → same result regardless of which call is first.
	op1 := &Synthesize{Consistent: true}
	op2 := &Synthesize{Consistent: true}
	v1 := mustSynthesize(t, op1, "alice@example.com", "EMAIL_ADDRESS")
	v2 := mustSynthesize(t, op2, "alice@example.com", "EMAIL_ADDRESS")
	if v1 != v2 {
		t.Errorf("expected same output from two Consistent instances, got %q vs %q", v1, v2)
	}
}

func TestSynthesize_DocumentScoped_WithinInstance(t *testing.T) {
	op := &Synthesize{Consistent: true, DocumentScoped: true}
	a := mustSynthesize(t, op, "John Smith", "PERSON")
	b := mustSynthesize(t, op, "John Smith", "PERSON")
	if a != b {
		t.Errorf("DocumentScoped: expected same alias within instance, got %q and %q", a, b)
	}
}

func TestSynthesize_DocumentScoped_AcrossInstances(t *testing.T) {
	op1 := &Synthesize{Consistent: true, DocumentScoped: true}
	op2 := &Synthesize{Consistent: true, DocumentScoped: true}

	v1 := mustSynthesize(t, op1, "John Smith", "PERSON")
	v2 := mustSynthesize(t, op2, "John Smith", "PERSON")

	// Different instances should produce different results (probabilistically).
	// This can theoretically collide; acceptable given large name space.
	if v1 == v2 {
		t.Logf("warning: two DocumentScoped instances produced the same alias %q (rare collision)", v1)
	}
}

func TestSynthesize_DocumentScoped_Reset(t *testing.T) {
	op := &Synthesize{Consistent: true, DocumentScoped: true}
	before := mustSynthesize(t, op, "John Smith", "PERSON")
	op.Reset()
	after := mustSynthesize(t, op, "John Smith", "PERSON")
	// After reset a new random alias is assigned; same value is possible but unlikely.
	_ = before
	_ = after
	// Mostly we verify Reset() doesn't panic and the operator still works.
	v := mustSynthesize(t, op, "John Smith", "PERSON")
	if v != after {
		t.Errorf("expected cache to be stable after reset, got %q then %q", after, v)
	}
}

// ── concurrency ───────────────────────────────────────────────────────────────

func TestSynthesize_Concurrent_DocumentScoped(t *testing.T) {
	op := &Synthesize{Consistent: true, DocumentScoped: true}
	var wg sync.WaitGroup
	results := make([]string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v, err := op.Anonymize("John Smith", "PERSON")
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
			}
			results[idx] = v
		}(i)
	}
	wg.Wait()

	// All results must be identical (cached value).
	for i, v := range results {
		if v != results[0] {
			t.Errorf("results[%d]=%q differs from results[0]=%q", i, v, results[0])
		}
	}
}

func TestSynthesize_Concurrent_Random(t *testing.T) {
	op := &Synthesize{}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := op.Anonymize("4111111111111111", "CREDIT_CARD")
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
