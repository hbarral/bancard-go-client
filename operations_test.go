package bancard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newOpCaptureVPOS returns a test server responding with respJSON after
// recording the request body, plus a client pointed at it.
func newOpCaptureVPOS(t *testing.T, respJSON string) (*Client, *singleBuyCapture) {
	t.Helper()
	cap := &singleBuyCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(t, r)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respJSON)
	}))
	t.Cleanup(srv.Close)
	return clientAt(t, srv), cap
}

func TestGetConfirmationSuccess(t *testing.T) {
	c, cap := newOpCaptureVPOS(t, `{
		"status": "success",
		"confirmation": {
			"shop_process_id": "54322",
			"response": "S",
			"amount": "10330.00",
			"currency": "PYG",
			"response_code": "00",
			"response_description": "Transacción aprobada.",
			"ticket_number": "123456789123456"
		},
		"messages": [{"key": "SomeInfo", "level": "info", "dsc": "d"}]
	}`)

	status, err := c.GetConfirmation(context.Background(), 54322)
	if err != nil {
		t.Fatalf("GetConfirmation: unexpected error: %v", err)
	}

	if status.Status != "success" {
		t.Errorf("Status = %q, want \"success\"", status.Status)
	}
	if status.Confirmation == nil {
		t.Fatal("Confirmation = nil, want the confirmation object")
	}
	cf := status.Confirmation
	if cf.ShopProcessID != 54322 {
		t.Errorf("ShopProcessID = %d, want 54322", cf.ShopProcessID)
	}
	if !cf.Approved() {
		t.Errorf("Approved() = false, want true")
	}
	if len(status.Messages) != 1 || status.Messages[0].Key != "SomeInfo" {
		t.Errorf("Messages = %v, want one SomeInfo message", status.Messages)
	}

	// Request shape: correct path, token formula, integer shop_process_id.
	if cap.path != singleBuyConfirmationsPath {
		t.Errorf("request path = %q, want %q", cap.path, singleBuyConfirmationsPath)
	}
	op := cap.operation(t)
	wantToken := referenceToken(t, testPrivateKey, "54322", "get_confirmation")
	if got, _ := op["token"].(string); got != wantToken {
		t.Errorf("operation.token = %q, want %q", got, wantToken)
	}
	if got, ok := op["shop_process_id"].(float64); !ok || int64(got) != 54322 {
		t.Errorf("operation.shop_process_id = %v, want integer 54322", op["shop_process_id"])
	}
}

func TestGetConfirmationNoConfirmation(t *testing.T) {
	c, _ := newOpCaptureVPOS(t, `{"status": "success"}`)

	status, err := c.GetConfirmation(context.Background(), 54322)
	if err != nil {
		t.Fatalf("GetConfirmation: unexpected error: %v", err)
	}
	if status.Confirmation != nil {
		t.Errorf("Confirmation = %v, want nil when none exists", status.Confirmation)
	}
}

func TestGetConfirmationAPIError(t *testing.T) {
	c, _ := newOpCaptureVPOS(t, `{
		"status": "error",
		"messages": [{"key": "BuyNotFoundError", "level": "error", "dsc": "Buy Not Found"}]
	}`)

	status, err := c.GetConfirmation(context.Background(), 54322)
	if status != nil {
		t.Fatal("GetConfirmation returned a response alongside an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GetConfirmation error = %v, want *APIError", err)
	}
	if !apiErr.Has("BuyNotFoundError") {
		t.Errorf("APIError should contain BuyNotFoundError, messages: %v", apiErr.Messages)
	}
}

func TestGetConfirmationValidation(t *testing.T) {
	c, _ := newOpCaptureVPOS(t, `{}`)

	for _, id := range []int64{0, -1} {
		if _, err := c.GetConfirmation(context.Background(), id); err == nil {
			t.Errorf("GetConfirmation(%d) should fail validation", id)
		}
	}
}

func TestRollbackSuccess(t *testing.T) {
	c, cap := newOpCaptureVPOS(t, `{
		"status": "success",
		"messages": [{"key": "RollbackSuccessful", "level": "info", "dsc": "Rollback correcto."}]
	}`)

	if err := c.Rollback(context.Background(), 54322); err != nil {
		t.Fatalf("Rollback: unexpected error: %v", err)
	}

	// Request shape: correct path, rollback token (fixed 0.00 amount),
	// string shop_process_id per the specification.
	if cap.path != singleBuyRollbackPath {
		t.Errorf("request path = %q, want %q", cap.path, singleBuyRollbackPath)
	}
	op := cap.operation(t)
	wantToken := referenceToken(t, testPrivateKey, "54322", "rollback", "0.00")
	if got, _ := op["token"].(string); got != wantToken {
		t.Errorf("operation.token = %q, want %q", got, wantToken)
	}
	if got, _ := op["shop_process_id"].(string); got != "54322" {
		t.Errorf("operation.shop_process_id = %v (%T), want string \"54322\"", op["shop_process_id"], op["shop_process_id"])
	}
}

func TestRollbackSemantics(t *testing.T) {
	tests := []struct {
		name     string
		respJSON string
		wantErr  bool
		wantKey  string // expected message key on the returned *APIError
	}{
		{
			name:     "rollback successful",
			respJSON: `{"status": "success"}`,
			wantErr:  false,
		},
		{
			// The specification mandates treating PaymentNotFoundError as
			// success: no payment existed.
			name: "payment not found is success",
			respJSON: `{"status": "error", "messages": [
				{"key": "PaymentNotFoundError", "level": "error", "dsc": "No payment found."}
			]}`,
			wantErr: false,
		},
		{
			name: "transaction already confirmed is a typed error",
			respJSON: `{"status": "error", "messages": [
				{"key": "TransactionAlreadyConfirmed", "level": "error", "dsc": "already couponed"}
			]}`,
			wantErr: true,
			wantKey: MessageKeyTransactionAlreadyConfirmed,
		},
		{
			name: "already rollbacked is a typed error",
			respJSON: `{"status": "error", "messages": [
				{"key": "AlreadyRollbackedError", "level": "error", "dsc": "previous rollback exists"}
			]}`,
			wantErr: true,
			wantKey: MessageKeyAlreadyRollbacked,
		},
		{
			name: "other errors are returned as APIError",
			respJSON: `{"status": "error", "messages": [
				{"key": "InvalidTokenError", "level": "error", "dsc": "bad token"}
			]}`,
			wantErr: true,
			wantKey: MessageKeyInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newOpCaptureVPOS(t, tt.respJSON)

			err := c.Rollback(context.Background(), 54322)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Rollback error = %v, want nil", err)
				}
				return
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Rollback error = %v, want *APIError", err)
			}
			if !apiErr.Has(tt.wantKey) {
				t.Errorf("APIError should contain %q, messages: %v", tt.wantKey, apiErr.Messages)
			}
		})
	}
}

func TestRollbackValidation(t *testing.T) {
	c, _ := newOpCaptureVPOS(t, `{}`)

	for _, id := range []int64{0, -1} {
		if err := c.Rollback(context.Background(), id); err == nil {
			t.Errorf("Rollback(%d) should fail validation", id)
		} else if !strings.Contains(err.Error(), "shop_process_id") {
			t.Errorf("Rollback(%d) error = %v, should mention shop_process_id", id, err)
		}
	}
}

func TestRollbackNetworkError(t *testing.T) {
	cap := &singleBuyCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(t, r)
		fmt.Fprint(w, `{}`)
	}))
	c := clientAt(t, srv)
	srv.Close() // fail every request

	err := c.Rollback(context.Background(), 54322)
	if err == nil {
		t.Fatal("Rollback should fail when VPOS is unreachable")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("transport errors must not surface as *APIError: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "bancard:") {
		t.Errorf("error = %v, want a bancard-prefixed error", err)
	}
}
