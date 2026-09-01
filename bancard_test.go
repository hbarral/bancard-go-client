package bancard

import (
	"net/http"
	"testing"
	"time"
)

func TestNewValidatesCredentials(t *testing.T) {
	shortPriv := "012345678901234567890123456789012345678" // 39 chars

	tests := []struct {
		name       string
		publicKey  string
		privateKey string
		env        Environment
		wantErr    error
	}{
		{"valid", testPublicKey, testPrivateKey, Staging, nil},
		{"invalid public key too short", "short", testPrivateKey, Staging, ErrInvalidPublicKey},
		{"invalid public key bad chars", "kR6oAQoIYCqUZLAivLQgac3lO7mv5bX!", testPrivateKey, Staging, ErrInvalidPublicKey},
		{"invalid private key too short", testPublicKey, shortPriv, Staging, ErrInvalidPrivateKey},
		{"invalid environment", testPublicKey, testPrivateKey, Environment("https://example.com"), ErrInvalidEnvironment},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.publicKey, tt.privateKey, tt.env)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("New error = %v, want %v", err, tt.wantErr)
				}
				if c != nil {
					t.Errorf("New returned non-nil client alongside error")
				}
				return
			}
			if err != nil {
				t.Fatalf("New: unexpected error: %v", err)
			}
			if c.Environment() != tt.env {
				t.Errorf("Environment() = %q, want %q", c.Environment(), tt.env)
			}
		})
	}
}

func TestNewDefaults(t *testing.T) {
	c, err := New(testPublicKey, testPrivateKey, Production)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if c.httpClient == nil {
		t.Fatal("default http client is nil")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("default http client timeout = %v, want 30s", c.httpClient.Timeout)
	}
	if c.userAgent != "bancard-go-client" {
		t.Errorf("default user agent = %q, want %q", c.userAgent, "bancard-go-client")
	}
}

func TestOptions(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}

	c, err := New(testPublicKey, testPrivateKey, Staging,
		WithHTTPClient(custom),
		WithUserAgent("my-webapp/1.0"),
	)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if c.httpClient != custom {
		t.Error("WithHTTPClient: custom client not used")
	}
	if c.userAgent != "my-webapp/1.0" {
		t.Errorf("WithUserAgent: user agent = %q, want %q", c.userAgent, "my-webapp/1.0")
	}

	// Options with zero values must not override defaults.
	c2, err := New(testPublicKey, testPrivateKey, Staging, WithHTTPClient(nil), WithUserAgent(""))
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if c2.httpClient == custom {
		t.Error("WithHTTPClient(nil) must be ignored")
	}
	if c2.userAgent != "bancard-go-client" {
		t.Errorf("WithUserAgent(\"\") must keep default, got %q", c2.userAgent)
	}
}
