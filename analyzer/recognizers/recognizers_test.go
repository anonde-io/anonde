package recognizers_test

import (
	"context"
	"testing"

	"github.com/anonde-io/anonde/analyzer/recognizers"
)

func TestEmailRecognizer(t *testing.T) {
	rec := recognizers.NewEmailRecognizer()
	results, _ := rec.Analyze(context.Background(), "contact me at foo@bar.com please", nil, "en")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].EntityType != "EMAIL_ADDRESS" {
		t.Errorf("unexpected entity type %q", results[0].EntityType)
	}
}

func TestCreditCardLuhn(t *testing.T) {
	rec := recognizers.NewCreditCardRecognizer()
	results, _ := rec.Analyze(context.Background(), "my card is 4111111111111111 ok", nil, "en")
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Score != 1.0 {
		t.Errorf("expected score 1.0 (Luhn pass), got %.2f", results[0].Score)
	}
}

func TestIBANValid(t *testing.T) {
	rec := recognizers.NewIBANRecognizer()
	results, _ := rec.Analyze(context.Background(), "GB29NWBK60161331926819", nil, "en")
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Score != 1.0 {
		t.Errorf("expected score 1.0 for valid IBAN, got %.2f", results[0].Score)
	}
}

func TestSSNRecognizer(t *testing.T) {
	rec := recognizers.NewUSSocialSecurityRecognizer()
	tests := []struct {
		text  string
		count int
	}{
		{"SSN: 123-45-6789", 1},
		{"invalid: 000-45-6789", 0}, // area 000 invalid
		{"invalid: 666-45-6789", 0}, // area 666 invalid
		{"invalid: 123-00-6789", 0}, // group 00 invalid
		{"invalid: 123-45-0000", 0}, // serial 0000 invalid
	}
	for _, tt := range tests {
		results, _ := rec.Analyze(context.Background(), tt.text, nil, "en")
		if len(results) != tt.count {
			t.Errorf("%q: expected %d, got %d", tt.text, tt.count, len(results))
		}
	}
}

func TestIPAddressRecognizer(t *testing.T) {
	rec := recognizers.NewIPAddressRecognizer()
	results, _ := rec.Analyze(context.Background(), "server at 192.168.1.1 and 10.0.0.1", nil, "en")
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}
}

func TestCryptoRecognizer(t *testing.T) {
	rec := recognizers.NewCryptoRecognizer()
	results, _ := rec.Analyze(context.Background(), "wallet 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", nil, "en")
	if len(results) != 1 {
		t.Errorf("expected 1, got %d", len(results))
	}
}
