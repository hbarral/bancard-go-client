package bancard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// validSingleBuyReq is a minimal, valid request used as the base for tests.
func validSingleBuyReq() *SingleBuyRequest {
	return &SingleBuyRequest{
		ShopProcessID: 54322,
		Amount:        "10330.00",
		Currency:      "PYG",
		Description:   "Ejemplo de pago",
		ReturnURL:     "http://www.example.com/finish",
	}
}

// singleBuyCapture records what the fake VPOS server received.
type singleBuyCapture struct {
	mu     sync.Mutex
	path   string
	method string
	body   map[string]any
}

func (cap *singleBuyCapture) record(t *testing.T, r *http.Request) {
	t.Helper()
	dec := json.NewDecoder(r.Body)
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		t.Errorf("fake VPOS: cannot decode request body: %v", err)
	}
	cap.mu.Lock()
	cap.path = r.URL.Path
	cap.method = r.Method
	cap.body = body
	cap.mu.Unlock()
}

func (cap *singleBuyCapture) operation(t *testing.T) map[string]any {
	t.Helper()
	cap.mu.Lock()
	defer cap.mu.Unlock()
	op, ok := cap.body["operation"].(map[string]any)
	if !ok {
		t.Fatalf("request body has no operation object: %v", cap.body)
	}
	return op
}

// newFakeVPOS returns a test server responding with respJSON to every
// request, after recording it in cap.
func newFakeVPOS(t *testing.T, cap *singleBuyCapture, respJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(t, r)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// clientAt returns a client pointing at the given test server URL. It
// constructs the Client directly (same package) because New only accepts
// the Staging and Production environments.
func clientAt(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return &Client{
		publicKey:  testPublicKey,
		privateKey: testPrivateKey,
		env:        Environment(srv.URL),
		httpClient: srv.Client(),
		userAgent:  "bancard-go-client-test",
	}
}

func TestSingleBuySuccess(t *testing.T) {
	cap := &singleBuyCapture{}
	srv := newFakeVPOS(t, cap, `{"status":"success","process_id":"i5fn*lx6niQel0QzWK1g"}`)
	c := clientAt(t, srv)

	resp, err := c.SingleBuy(context.Background(), validSingleBuyReq())
	if err != nil {
		t.Fatalf("SingleBuy: unexpected error: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("Status = %q, want %q", resp.Status, "success")
	}
	if resp.ProcessID != "i5fn*lx6niQel0QzWK1g" {
		t.Errorf("ProcessID = %q, want %q", resp.ProcessID, "i5fn*lx6niQel0QzWK1g")
	}

	if cap.method != http.MethodPost {
		t.Errorf("request method = %q, want POST", cap.method)
	}
	if cap.path != singleBuyPath {
		t.Errorf("request path = %q, want %q", cap.path, singleBuyPath)
	}

	cap.mu.Lock()
	pub, _ := cap.body["public_key"].(string)
	cap.mu.Unlock()
	if pub != testPublicKey {
		t.Errorf("public_key = %q, want %q", pub, testPublicKey)
	}

	op := cap.operation(t)
	wantToken := referenceToken(t, testPrivateKey, "54322", "10330.00", "PYG")
	if got, _ := op["token"].(string); got != wantToken {
		t.Errorf("operation.token = %q, want %q", got, wantToken)
	}
	if got, ok := op["shop_process_id"].(float64); !ok || int64(got) != 54322 {
		t.Errorf("operation.shop_process_id = %v (%T), want integer 54322", op["shop_process_id"], op["shop_process_id"])
	}
	if got, _ := op["amount"].(string); got != "10330.00" {
		t.Errorf("operation.amount = %v, want \"10330.00\"", op["amount"])
	}
	if got, _ := op["currency"].(string); got != "PYG" {
		t.Errorf("operation.currency = %v, want \"PYG\"", op["currency"])
	}
	if got, _ := op["return_url"].(string); got != "http://www.example.com/finish" {
		t.Errorf("operation.return_url = %v, want the configured return URL", op["return_url"])
	}

	// Optional fields must be omitted from the wire format.
	for _, field := range []string{"cancel_url", "iva_amount", "additional_data", "preauthorization", "simple", "billing", "extra_response_attributes"} {
		if _, present := op[field]; present {
			t.Errorf("operation.%s must be omitted when unset, got %v", field, op[field])
		}
	}
}

func TestSingleBuyFlagsAndOptionalFields(t *testing.T) {
	cap := &singleBuyCapture{}
	srv := newFakeVPOS(t, cap, `{"status":"success","process_id":"p"}`)
	c := clientAt(t, srv)

	req := validSingleBuyReq()
	req.CancelURL = "http://www.example.com/cancel"
	req.IvaAmount = "1033.00"
	req.AdditionalData = "0981123456"
	req.Zimple = true
	req.Preauthorization = true
	req.ExtraResponseAttributes = []string{"payment_card_type"}

	if _, err := c.SingleBuy(context.Background(), req); err != nil {
		t.Fatalf("SingleBuy: unexpected error: %v", err)
	}

	op := cap.operation(t)
	for field, want := range map[string]string{
		"cancel_url":       "http://www.example.com/cancel",
		"iva_amount":       "1033.00",
		"additional_data":  "0981123456",
		"preauthorization": "S",
		"simple":           "S",
	} {
		if got, _ := op[field].(string); got != want {
			t.Errorf("operation.%s = %v, want %q", field, op[field], want)
		}
	}
	attrs, ok := op["extra_response_attributes"].([]any)
	if !ok || len(attrs) != 1 || attrs[0] != "payment_card_type" {
		t.Errorf("operation.extra_response_attributes = %v, want [\"payment_card_type\"]", op["extra_response_attributes"])
	}
}

func TestSingleBuyBillingSerialization(t *testing.T) {
	cap := &singleBuyCapture{}
	srv := newFakeVPOS(t, cap, `{"status":"success","process_id":"p"}`)
	c := clientAt(t, srv)

	req := validSingleBuyReq()
	req.Billing = &Billing{
		ClientRuc:               "123456-1",
		ClientName:              "JUAN GONZALEZ",
		ClientEmail:             "juangonzalez@mail.com.py",
		CommerceStamp:           "12559969",
		CommerceExpeditionPoint: "001",
		CommerceEstablishment:   "002",
		Details: []BillingDetail{
			{Description: "item 1", Amount: "10000.00", IvaRate: 10, TotalItems: 1},
			{Description: "item 2", Amount: "330.00", IvaRate: 10, TotalItems: 1},
		},
	}

	if _, err := c.SingleBuy(context.Background(), req); err != nil {
		t.Fatalf("SingleBuy: unexpected error: %v", err)
	}

	op := cap.operation(t)
	billing, ok := op["billing"].(map[string]any)
	if !ok {
		t.Fatalf("operation.billing missing or not an object: %v", op["billing"])
	}
	for field, want := range map[string]string{
		"client_ruc":                "123456-1",
		"commerce_stamp":            "12559969",
		"commerce_expedition_point": "001",
		"commerce_establishment":    "002",
	} {
		if got, _ := billing[field].(string); got != want {
			t.Errorf("billing.%s = %v, want %q", field, billing[field], want)
		}
	}
	details, ok := billing["details"].([]any)
	if !ok || len(details) != 2 {
		t.Fatalf("billing.details = %v, want 2 items", billing["details"])
	}
}

func TestSingleBuyAPIError(t *testing.T) {
	srv := newFakeVPOS(t, &singleBuyCapture{}, `{
		"status": "error",
		"messages": [
			{"key": "InvalidTokenError", "level": "error", "dsc": "Incorrect token generation."}
		]
	}`)
	c := clientAt(t, srv)

	resp, err := c.SingleBuy(context.Background(), validSingleBuyReq())
	if resp != nil {
		t.Fatalf("SingleBuy returned a response alongside an error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("SingleBuy error = %v, want *APIError", err)
	}
	if apiErr.Status != "error" {
		t.Errorf("APIError.Status = %q, want \"error\"", apiErr.Status)
	}
	if !apiErr.Has(MessageKeyInvalidToken) {
		t.Errorf("APIError should contain %q, messages: %v", MessageKeyInvalidToken, apiErr.Messages)
	}
	if !strings.Contains(apiErr.Error(), "InvalidTokenError") {
		t.Errorf("APIError.Error() = %q, should mention the message key", apiErr.Error())
	}
}

func TestSingleBuySuccessWithoutProcessID(t *testing.T) {
	srv := newFakeVPOS(t, &singleBuyCapture{}, `{"status":"success"}`)
	c := clientAt(t, srv)

	_, err := c.SingleBuy(context.Background(), validSingleBuyReq())
	if err == nil {
		t.Fatal("SingleBuy should fail when success response has no process_id")
	}
	if !strings.Contains(err.Error(), "process_id") {
		t.Errorf("error = %v, should mention missing process_id", err)
	}
}

func TestSingleBuyNetworkError(t *testing.T) {
	srv := newFakeVPOS(t, &singleBuyCapture{}, `{}`)
	c := clientAt(t, srv)
	srv.Close() // fail every request

	_, err := c.SingleBuy(context.Background(), validSingleBuyReq())
	if err == nil {
		t.Fatal("SingleBuy should fail when VPOS is unreachable")
	}
	if !strings.HasPrefix(err.Error(), "bancard:") {
		t.Errorf("error = %v, want a bancard-prefixed error", err)
	}
}

func TestSingleBuyContextCancelled(t *testing.T) {
	srv := newFakeVPOS(t, &singleBuyCapture{}, `{"status":"success","process_id":"p"}`)
	c := clientAt(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.SingleBuy(ctx, validSingleBuyReq()); !errors.Is(err, context.Canceled) {
		t.Fatalf("SingleBuy error = %v, want context.Canceled", err)
	}
}

func TestSingleBuyValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SingleBuyRequest)
		wantIn string
	}{
		{
			name:   "no shop_process_id",
			mutate: func(r *SingleBuyRequest) { r.ShopProcessID = 0 },
			wantIn: "shop_process_id",
		},
		{
			name:   "negative shop_process_id",
			mutate: func(r *SingleBuyRequest) { r.ShopProcessID = -1 },
			wantIn: "shop_process_id",
		},
		{
			name:   "amount without decimals",
			mutate: func(r *SingleBuyRequest) { r.Amount = "10330" },
			wantIn: "amount",
		},
		{
			name:   "amount with comma separator",
			mutate: func(r *SingleBuyRequest) { r.Amount = "10330,00" },
			wantIn: "amount",
		},
		{
			name:   "zero amount",
			mutate: func(r *SingleBuyRequest) { r.Amount = "0.00" },
			wantIn: "greater than zero",
		},
		{
			name:   "amount with three decimals",
			mutate: func(r *SingleBuyRequest) { r.Amount = "10330.000" },
			wantIn: "amount",
		},
		{
			name:   "empty currency",
			mutate: func(r *SingleBuyRequest) { r.Currency = "" },
			wantIn: "currency",
		},
		{
			name:   "lowercase currency",
			mutate: func(r *SingleBuyRequest) { r.Currency = "pyg" },
			wantIn: "currency",
		},
		{
			name:   "empty description",
			mutate: func(r *SingleBuyRequest) { r.Description = "" },
			wantIn: "description",
		},
		{
			name:   "description too long",
			mutate: func(r *SingleBuyRequest) { r.Description = strings.Repeat("x", 21) },
			wantIn: "max is 20",
		},
		{
			name:   "empty return_url",
			mutate: func(r *SingleBuyRequest) { r.ReturnURL = "" },
			wantIn: "return_url",
		},
		{
			name:   "return_url too long",
			mutate: func(r *SingleBuyRequest) { r.ReturnURL = "http://" + strings.Repeat("a", 250) },
			wantIn: "max is 255",
		},
		{
			name:   "cancel_url too long",
			mutate: func(r *SingleBuyRequest) { r.CancelURL = "http://" + strings.Repeat("a", 250) },
			wantIn: "max is 255",
		},
		{
			name:   "iva_amount wrong format",
			mutate: func(r *SingleBuyRequest) { r.IvaAmount = "10" },
			wantIn: "iva_amount",
		},
		{
			name:   "zimple without phone",
			mutate: func(r *SingleBuyRequest) { r.Zimple = true },
			wantIn: "zimple",
		},
		{
			name: "empty extra_response_attribute",
			mutate: func(r *SingleBuyRequest) {
				r.ExtraResponseAttributes = []string{""}
			},
			wantIn: "extra_response_attributes",
		},
		{
			name: "billing without details",
			mutate: func(r *SingleBuyRequest) {
				r.Billing = &Billing{CommerceStamp: "1", CommerceExpeditionPoint: "001", CommerceEstablishment: "002"}
			},
			wantIn: "details",
		},
		{
			name: "billing ruc without client name",
			mutate: func(r *SingleBuyRequest) {
				r.Billing = &Billing{
					ClientRuc:               "123456-1",
					ClientEmail:             "a@b.com",
					CommerceStamp:           "1",
					CommerceExpeditionPoint: "001",
					CommerceEstablishment:   "002",
					Details:                 []BillingDetail{{Description: "item", Amount: "10330.00", IvaRate: 10, TotalItems: 1}},
				}
			},
			wantIn: "client_name",
		},
		{
			name: "billing details total mismatch",
			mutate: func(r *SingleBuyRequest) {
				r.Billing = &Billing{
					ClientRuc:               "123456-1",
					ClientName:              "JUAN",
					ClientEmail:             "a@b.com",
					CommerceStamp:           "1",
					CommerceExpeditionPoint: "001",
					CommerceEstablishment:   "002",
					Details:                 []BillingDetail{{Description: "item", Amount: "100.00", IvaRate: 10, TotalItems: 1}},
				}
			},
			wantIn: "must match operation amount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validSingleBuyReq()
			tt.mutate(req)

			_, err := singleBuyValidationCall(t, req)
			if err == nil {
				t.Fatal("SingleBuy should fail validation")
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantIn)
			}
		})
	}
}

// singleBuyValidationCall runs SingleBuy against an unreachable endpoint;
// validation must fail before any network attempt.
func singleBuyValidationCall(t *testing.T, req *SingleBuyRequest) (*SingleBuyResponse, error) {
	t.Helper()
	c, err := New(testPublicKey, testPrivateKey, Staging, WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	return c.SingleBuy(context.Background(), req)
}

func TestSingleBuyConcurrentCalls(t *testing.T) {
	srv := newFakeVPOS(t, &singleBuyCapture{}, `{"status":"success","process_id":"p"}`)
	c := clientAt(t, srv)

	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			req := validSingleBuyReq()
			req.ShopProcessID = id
			if _, err := c.SingleBuy(context.Background(), req); err != nil {
				errs <- err
			}
		}(int64(i) + 1)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent SingleBuy: %v", err)
	}
}
