package recognizers

import (
	"context"
	"testing"
)

// analyzeSecret runs the recognizer and reports whether ANY span overlaps the
// given substring of text. Overlap (not exact-equality) is the right check for
// leak-rate semantics: a gold secret is "caught" if any predicted span covers
// it.
func analyzeSecret(t *testing.T, text, want string) bool {
	t.Helper()
	rec := NewSecretRecognizer()
	res, err := rec.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	ws := indexOf(text, want)
	if ws < 0 {
		t.Fatalf("test bug: substring %q not in text", want)
	}
	we := ws + len(want)
	for _, r := range res {
		if r.Start < we && r.End > ws { // overlap
			if r.EntityType != "SECRET" {
				t.Fatalf("got entity type %q, want SECRET", r.EntityType)
			}
			if r.Score < 0.9 {
				t.Fatalf("SECRET score %.2f too low to win conflicts", r.Score)
			}
			return true
		}
	}
	return false
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// hasAnySecret reports whether the recognizer flags anything at all.
func hasAnySecret(t *testing.T, text string) bool {
	t.Helper()
	rec := NewSecretRecognizer()
	res, err := rec.Analyze(context.Background(), text, nil, "en")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	return len(res) > 0
}

// --- DETECT: synthetic, fake-but-shaped tokens (NEVER real keys) -----------

func TestSecretDetect(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // substring that must be covered by a SECRET span
	}{
		{
			// Prefixes are split with + so the source has no contiguous
			// provider-token literal (GitHub push protection scans raw text);
			// the recognizer sees the assembled runtime string and still matches.
			name: "github pat",
			text: "export GITHUB_TOKEN=ghp_" + "0123456789abcdefABCDEFghijklmnopqrST",
			want: "ghp_" + "0123456789abcdefABCDEFghijklmnopqrST",
		},
		{
			name: "aws access key",
			text: "aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
			want: "AKIAIOSFODNN7EXAMPLE",
		},
		{
			name: "stripe live key",
			text: "STRIPE_KEY=sk_live_" + "abcdEFGH1234ijklMNOP5678",
			want: "sk_live_" + "abcdEFGH1234ijklMNOP5678",
		},
		{
			name: "google api key",
			text: "google_api_key: AIza" + "SyDfaKe1234567890abcdefghijKLMNOPqr",
			want: "AIza" + "SyDfaKe1234567890abcdefghijKLMNOPqr",
		},
		{
			name: "jwt header token",
			text: "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummysignature1234567890",
			want: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummysignature1234567890",
		},
		{
			name: "pem private key block",
			text: "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAfake\n-----END RSA PRIVATE KEY-----",
			want: "-----BEGIN RSA PRIVATE KEY-----",
		},
		{
			name: "generic api_key assignment high entropy",
			text: `api_key = "Zx9Kq2Lp7Mv3Nw8Rt5Yu1Bd6Hf4Jg0"`,
			want: "Zx9Kq2Lp7Mv3Nw8Rt5Yu1Bd6Hf4Jg0",
		},
		// synth_logs-shaped: a 40-char hex assigned to key= (the hard case —
		// shape-identical to a git SHA, distinguished only by context).
		{
			name: "synth 40-hex api key context-anchored",
			text: "WARN api.key.used account=usr_d7bee4 key=1b2abb031cea6973efd2a83d4815df06a27d290c endpoint=https://x",
			want: "1b2abb031cea6973efd2a83d4815df06a27d290c",
		},
		// synth_logs-shaped: opaque JWT (no eyJ header) anchored by jwt=.
		{
			name: "synth opaque jwt context-anchored",
			text: "jwt=sTFDo8w9oS4VxRHnWyIVvjtjjj_hR85Btboi.Dn6XexPiBULs68OAMeKgJPgu7ZLi1TWpOTzLWySwXRvPZcpZqKaVNQfGk87.ukuzcqCSf3BFhDw1nQwWT3lPhfIh1rr0k33IUS6cPb",
			want: "sTFDo8w9oS4VxRHnWyIVvjtjjj_hR85Btboi.Dn6XexPiBULs68OAMeKgJPgu7ZLi1TWpOTzLWySwXRvPZcpZqKaVNQfGk87.ukuzcqCSf3BFhDw1nQwWT3lPhfIh1rr0k33IUS6cPb",
		},
		// synth_logs-shaped: opaque bearer/session token anchored by session=.
		{
			name: "synth session token context-anchored",
			text: "session.created user=jennifer ip=5db2 session=3aqDOWh_H5EK2yMRNadsObMPoMaNfqbR ua=Mozilla",
			want: "3aqDOWh_H5EK2yMRNadsObMPoMaNfqbR",
		},
		// synth_logs-shaped: oauth client secret anchored by client_secret=.
		{
			name: "synth oauth client secret",
			text: "oauth.exchange vendor=stripe account=ACC98 client_secret=stripe_cs_Kq2Lp7Mv3Nw8Rt5Yu1Bd6Hf4Jg0Zx",
			want: "stripe_cs_Kq2Lp7Mv3Nw8Rt5Yu1Bd6Hf4Jg0Zx",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !analyzeSecret(t, c.text, c.want) {
				t.Errorf("expected SECRET covering %q in %q, got none", c.want, c.text)
			}
		})
	}
}

// --- KEEP: must NOT flag (precision / leak-safety) --------------------------

func TestSecretKeep(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{
			name: "bare git sha 40 hex no context",
			text: "Fixed in commit 1b2abb031cea6973efd2a83d4815df06a27d290c on main.",
		},
		{
			name: "short git sha",
			text: "see commit 9390bb6 for the change",
		},
		{
			name: "bare uuid no context",
			text: "request id 550e8400-e29b-41d4-a716-446655440000 completed",
		},
		{
			name: "ordinary base64 blob no secret context",
			text: "avatar=iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ thumbnail",
		},
		{
			name: "version string",
			text: "upgraded library to v12.4.1-rc2 today",
		},
		{
			name: "semver assigned to non-secret field",
			text: "version=2.10.34 build=release",
		},
		{
			name: "normal english sentence",
			text: "The quick brown fox jumps over the lazy dog near the river bank.",
		},
		{
			name: "clinical prose no secrets",
			text: "Der Patient wurde am Montag entlassen und erhielt eine Folgeverordnung.",
		},
		{
			name: "monotone run assigned to token",
			text: "token=aaaaaaaaaaaaaaaaaaaaaaaa placeholder",
		},
		{
			name: "locale tag",
			text: "Accept-Language: de-DE preferred",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if hasAnySecret(t, c.text) {
				rec := NewSecretRecognizer()
				res, _ := rec.Analyze(context.Background(), c.text, nil, "en")
				t.Errorf("false positive: %q flagged %d secret span(s): %+v", c.text, len(res), res)
			}
		})
	}
}
