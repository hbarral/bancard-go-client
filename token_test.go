package bancard

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"
)

// test keys match the VPOS key formats: public [a-zA-Z0-9]{32},
// private [a-zA-Z0-9T1]{40}.
const (
	testPublicKey  = "kR6oAQoIYCqUZLAivLQgac3lO7mv5bXZ"
	testPrivateKey = "0123456789012345678901234567890123456789"
)

func mustClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(testPublicKey, testPrivateKey, Staging)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	return c
}

// referenceToken computes the expected token independently from the
// implementation under test, mirroring the formulas in the VPOS spec.
func referenceToken(t *testing.T, parts ...string) string {
	t.Helper()
	h := md5.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%s", p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestTokenFormulas(t *testing.T) {
	c := mustClient(t)

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "single_buy",
			got:  c.singleBuyToken(3332134, "130.00", "PYG"),
			want: referenceToken(t, testPrivateKey, "3332134", "130.00", "PYG"),
		},
		{
			name: "single_buy large amount",
			got:  c.singleBuyToken(54322, "10330.00", "PYG"),
			want: referenceToken(t, testPrivateKey, "54322", "10330.00", "PYG"),
		},
		{
			name: "confirm",
			got:  c.confirmToken(12313, "10100.00", "PYG"),
			want: referenceToken(t, testPrivateKey, "12313", "confirm", "10100.00", "PYG"),
		},
		{
			name: "get_confirmation",
			got:  c.getConfirmationToken(54322),
			want: referenceToken(t, testPrivateKey, "54322", "get_confirmation"),
		},
		{
			name: "rollback always uses 0.00 regardless of amount",
			got:  c.rollbackToken(54322),
			want: referenceToken(t, testPrivateKey, "54322", "rollback", "0.00"),
		},
		{
			name: "rollback different shop_process_id",
			got:  c.rollbackToken(1),
			want: referenceToken(t, testPrivateKey, "1", "rollback", "0.00"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("token mismatch:\n got  %s\n want %s", tt.got, tt.want)
			}
			if len(tt.got) != 32 {
				t.Errorf("token length = %d, want 32 (md5 hex)", len(tt.got))
			}
		})
	}
}

func TestVerifyConfirmToken(t *testing.T) {
	c := mustClient(t)

	valid := c.confirmToken(12313, "10100.00", "PYG")

	tests := []struct {
		name   string
		token  string
		shopID int64
		amount string
		valid  bool
	}{
		{"valid token", valid, 12313, "10100.00", true},
		{"wrong amount", valid, 12313, "9999.00", false},
		{"wrong shop_process_id", valid, 99999, "10100.00", false},
		{"tampered token", "0" + valid[1:], 12313, "10100.00", false},
		{"empty token", "", 12313, "10100.00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.verifyConfirmToken(tt.token, tt.shopID, tt.amount, "PYG")
			if got != tt.valid {
				t.Errorf("verifyConfirmToken(%q, %d, %q) = %v, want %v",
					tt.token, tt.shopID, tt.amount, got, tt.valid)
			}
		})
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  string
	}{
		{"integer", 10330, "10330.00"},
		{"one decimal", 130.5, "130.50"},
		{"two decimals", 130.99, "130.99"},
		{"zero", 0, "0.00"},
		{"large value no scientific notation", 1e12, "1000000000000.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAmount(tt.input); got != tt.want {
				t.Errorf("formatAmount(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
