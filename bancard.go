// Package bancard provides a Go client for the Bancard VPOS 2.0
// eCommerce payment gateway API.
//
// This version implements the Occasional Payment flow: initiating a
// single_buy payment order, verifying incoming transaction confirmations,
// querying confirmation status, and rolling back transactions.
//
// Card data never passes through this package: it is entered by the user
// directly in Bancard's PCI-compliant iframe.
package bancard

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

// Environment identifies a VPOS deployment. The zero value is not usable;
// use Staging or Production.
type Environment string

const (
	// Staging is the VPOS test environment.
	Staging Environment = "https://vpos.infonet.com.py:8888"
	// Production is the live VPOS environment.
	Production Environment = "https://vpos.infonet.com.py"
)

// APIVersion is the VPOS API version this client targets.
const APIVersion = "0.3"

var (
	publicKeyPattern  = regexp.MustCompile(`^[a-zA-Z0-9]{32}$`)
	privateKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9T1]{40}$`)
)

// ErrInvalidPublicKey is returned by New when the public key does not
// match the format expected by VPOS: [a-zA-Z0-9]{32}.
var ErrInvalidPublicKey = fmt.Errorf("bancard: public key must match %s", publicKeyPattern.String())

// ErrInvalidPrivateKey is returned by New when the private key does not
// match the format expected by VPOS: [a-zA-Z0-9T1]{40}.
var ErrInvalidPrivateKey = fmt.Errorf("bancard: private key must match %s", privateKeyPattern.String())

// ErrInvalidEnvironment is returned by New when the environment is neither
// Staging nor Production.
var ErrInvalidEnvironment = fmt.Errorf("bancard: environment must be Staging or Production")

// Client is a VPOS 2.0 client for a single merchant in a single
// environment. It is immutable and safe for concurrent use; create one per
// application and reuse it.
type Client struct {
	publicKey  string
	privateKey string
	env        Environment
	httpClient *http.Client
	userAgent  string
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithHTTPClient sets the *http.Client used for requests to VPOS. Useful
// for custom transports or timeouts in tests. Defaults to a client with a
// 30 second timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithUserAgent sets the User-Agent header sent with every request to
// VPOS.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// New creates a Client for the given merchant credentials and environment.
// Keys are obtained from the merchant portal at
// https://comercios.bancard.com.py. An error is returned if any argument
// is invalid, so misconfiguration fails fast instead of producing
// InvalidTokenError responses from VPOS.
func New(publicKey, privateKey string, env Environment, opts ...Option) (*Client, error) {
	if !publicKeyPattern.MatchString(publicKey) {
		return nil, ErrInvalidPublicKey
	}
	if !privateKeyPattern.MatchString(privateKey) {
		return nil, ErrInvalidPrivateKey
	}
	if env != Staging && env != Production {
		return nil, ErrInvalidEnvironment
	}

	c := &Client{
		publicKey:  publicKey,
		privateKey: privateKey,
		env:        env,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  "bancard-go-client",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Environment returns the VPOS environment the client operates against.
func (c *Client) Environment() Environment {
	return c.env
}

// baseURL returns the environment base URL with no trailing slash.
func (c *Client) baseURL() string {
	return string(c.env)
}

// doPost sends a JSON-encoded operation payload to the given VPOS endpoint
// and decodes the JSON response into out. The public key and operation
// token are added automatically by the caller-supplied build funcs.
func (c *Client) doPost(ctx context.Context, path string, payload any, out any) error {
	url := c.baseURL() + path

	body, err := marshalJSON(payload)
	if err != nil {
		return fmt.Errorf("bancard: encode request for %s: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("bancard: build request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bancard: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if err := decodeJSON(resp.Body, out); err != nil {
		return fmt.Errorf("bancard: decode response from %s (status %d): %w", path, resp.StatusCode, err)
	}
	return nil
}
