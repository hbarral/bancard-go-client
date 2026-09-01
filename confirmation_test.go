package bancard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// confirmBody builds a single_buy_confirm request body with a valid token
// for the given fields, mirroring what VPOS sends.
func confirmBody(t *testing.T, shopProcessID, amount, currency string) string {
	t.Helper()
	token := referenceToken(t, testPrivateKey, shopProcessID, "confirm", amount, currency)
	return fmt.Sprintf(`{
		"operation": {
			"token": %q,
			"shop_process_id": %s,
			"response": "S",
			"response_details": "respuesta S",
			"extended_response_description": "respuesta extendida",
			"currency": %q,
			"amount": %q,
			"authorization_number": "123456",
			"ticket_number": "123456789123456",
			"iva_amount": "1100.0",
			"response_code": "00",
			"response_description": "Transacción aprobada.",
			"security_information": {
				"customer_ip": "123.123.123.123",
				"card_source": "I",
				"card_country": "Croacia",
				"version": "0.3",
				"risk_index": "0"
			},
			"billing_response": {
				"status": "success",
				"description": "Factura generada correctamente",
				"data": {"invoice_number": "001-001-0002563"}
			}
		}
	}`, token, shopProcessID, currency, amount)
}

func TestFlexInt64(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    int64
		wantErr bool
	}{
		{"number", `12313`, 12313, false},
		{"numeric string", `"12313"`, 12313, false},
		{"null", `null`, 0, false},
		{"empty string", `""`, 0, false},
		{"zero", `0`, 0, false},
		{"negative number", `-5`, -5, false},
		{"large number", `123456789123456`, 123456789123456, false},
		{"non-numeric string", `"abc"`, 0, true},
		{"float", `1.5`, 0, true},
		{"garbage", `{}`, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f flexInt64
			err := json.Unmarshal([]byte(tt.json), &f)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) succeeded, want error", tt.json)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s): unexpected error: %v", tt.json, err)
			}
			if int64(f) != tt.want {
				t.Errorf("flexInt64 = %d, want %d", f, tt.want)
			}
		})
	}
}

// TestConfirmationDecoding uses the specification's example payload with
// string-typed shop_process_id, ticket_number, and risk_index, plus an
// integer variant, verifying both decode into the same Confirmation.
func TestConfirmationDecoding(t *testing.T) {
	decode := func(body string) (*Confirmation, error) {
		c := mustClient(t)
		var got *Confirmation
		handler := c.ConfirmHandler(func(cf *Confirmation) error {
			got = cf
			return nil
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/confirm", bytes.NewReader([]byte(body)))
		handler(rec, req)
		if rec.Code != http.StatusOK {
			return nil, fmt.Errorf("handler status = %d, body %s", rec.Code, rec.Body.String())
		}
		return got, nil
	}

	stringBody := confirmBody(t, "12313", "10100.00", "PYG")

	intBody := fmt.Sprintf(`{
		"operation": {
			"token": %q,
			"shop_process_id": 12313,
			"response": "S",
			"amount": "10100.00",
			"currency": "PYG",
			"ticket_number": 123456789123456,
			"response_code": "00",
			"response_description": "Transacción aprobada.",
			"security_information": {"risk_index": 0, "card_source": "I"}
		}
	}`, referenceToken(t, testPrivateKey, "12313", "confirm", "10100.00", "PYG"))

	for name, body := range map[string]string{"string fields": stringBody, "integer fields": intBody} {
		t.Run(name, func(t *testing.T) {
			cf, err := decode(body)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}

			if cf.ShopProcessID != 12313 {
				t.Errorf("ShopProcessID = %d, want 12313", cf.ShopProcessID)
			}
			if cf.Response != "S" {
				t.Errorf("Response = %q, want \"S\"", cf.Response)
			}
			if cf.Amount != "10100.00" {
				t.Errorf("Amount = %q, want \"10100.00\"", cf.Amount)
			}
			if cf.Currency != "PYG" {
				t.Errorf("Currency = %q, want \"PYG\"", cf.Currency)
			}
			if cf.TicketNumber != "123456789123456" {
				t.Errorf("TicketNumber = %q, want \"123456789123456\"", cf.TicketNumber)
			}
			if cf.ResponseCode != "00" {
				t.Errorf("ResponseCode = %q, want \"00\"", cf.ResponseCode)
			}
			if cf.Security.RiskIndex != 0 {
				t.Errorf("RiskIndex = %d, want 0", cf.Security.RiskIndex)
			}
			if !cf.Approved() {
				t.Error("Approved() = false, want true for response S with code 00")
			}
		})
	}

	t.Run("full fields from spec example", func(t *testing.T) {
		cf, err := decode(stringBody)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}

		if cf.AuthorizationNumber != "123456" {
			t.Errorf("AuthorizationNumber = %q, want \"123456\"", cf.AuthorizationNumber)
		}
		if cf.IvaAmount != "1100.0" {
			t.Errorf("IvaAmount = %q, want \"1100.0\"", cf.IvaAmount)
		}
		if cf.Security.CustomerIP != "123.123.123.123" {
			t.Errorf("CustomerIP = %q, want \"123.123.123.123\"", cf.Security.CustomerIP)
		}
		if cf.Security.CardSource != "I" {
			t.Errorf("CardSource = %q, want \"I\"", cf.Security.CardSource)
		}
		if cf.Security.CardCountry != "Croacia" {
			t.Errorf("CardCountry = %q, want \"Croacia\"", cf.Security.CardCountry)
		}
		if cf.Security.Version != "0.3" {
			t.Errorf("Version = %q, want \"0.3\"", cf.Security.Version)
		}
		if cf.Billing == nil {
			t.Fatal("Billing is nil, want the billing_response object")
		}
		if cf.Billing.Status != "success" {
			t.Errorf("Billing.Status = %q, want \"success\"", cf.Billing.Status)
		}
		if got, _ := cf.Billing.Data["invoice_number"].(string); got != "001-001-0002563" {
			t.Errorf("Billing invoice_number = %v, want \"001-001-0002563\"", cf.Billing.Data["invoice_number"])
		}
	})
}

func TestConfirmHandlerResponses(t *testing.T) {
	c := mustClient(t)

	post := func(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/confirm", bytes.NewReader([]byte(body)))
		handler(rec, req)
		return rec
	}

	t.Run("valid token replies 200 with success status", func(t *testing.T) {
		var received *Confirmation
		handler := c.ConfirmHandler(func(cf *Confirmation) error {
			received = cf
			return nil
		})

		rec := post(handler, confirmBody(t, "12313", "10100.00", "PYG"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response body is not JSON: %v", err)
		}
		if resp["status"] != "success" {
			t.Errorf("status field = %q, want \"success\"", resp["status"])
		}
		if received == nil || received.ShopProcessID != 12313 {
			t.Errorf("callback not invoked with the confirmation: %v", received)
		}
	})

	t.Run("invalid token replies 403 and skips callback", func(t *testing.T) {
		called := false
		handler := c.ConfirmHandler(func(*Confirmation) error {
			called = true
			return nil
		})

		// Syntactically valid JSON whose token was computed with the
		// wrong amount: verification must fail.
		body := confirmBody(t, "12313", "10100.00", "PYG")
		validToken := referenceToken(t, testPrivateKey, "12313", "confirm", "10100.00", "PYG")
		wrongToken := referenceToken(t, testPrivateKey, "12313", "confirm", "9999.00", "PYG")
		body = strings.Replace(body, validToken, wrongToken, 1)
		rec := post(handler, body)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if called {
			t.Error("callback must not run for an invalid token")
		}
	})

	t.Run("malformed JSON replies 400", func(t *testing.T) {
		handler := c.ConfirmHandler(func(*Confirmation) error { return nil })
		rec := post(handler, `{"operation": `)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("GET is rejected with 405", func(t *testing.T) {
		handler := c.ConfirmHandler(func(*Confirmation) error { return nil })
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, "/confirm", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("callback error replies 500", func(t *testing.T) {
		handler := c.ConfirmHandler(func(*Confirmation) error {
			return errors.New("storage down")
		})
		rec := post(handler, confirmBody(t, "777", "500.00", "PYG"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response body is not JSON: %v", err)
		}
		if resp["status"] != "error" {
			t.Errorf("status field = %q, want \"error\"", resp["status"])
		}
	})

	t.Run("redelivery invokes callback again", func(t *testing.T) {
		calls := 0
		handler := c.ConfirmHandler(func(*Confirmation) error {
			calls++
			return nil
		})
		body := confirmBody(t, "999", "250.00", "PYG")

		for i := 0; i < 2; i++ {
			rec := post(handler, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("delivery %d: status = %d, want 200", i+1, rec.Code)
			}
		}
		if calls != 2 {
			t.Errorf("callback calls = %d, want 2 (idempotency is the app's job)", calls)
		}
	})
}

func TestConfirmationApproved(t *testing.T) {
	tests := []struct {
		name string
		resp string
		code string
		want bool
	}{
		{"approved", "S", "00", true},
		{"denied response", "N", "00", false},
		{"response S but code 51", "S", "51", false},
		{"denied insufficient funds", "N", "51", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := &Confirmation{Response: tt.resp, ResponseCode: tt.code}
			if got := cf.Approved(); got != tt.want {
				t.Errorf("Approved() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponseCodeDescription(t *testing.T) {
	if got := ResponseCodeDescription(ResponseCodeApproved); got != "TRANSACCIÓN APROBADA" {
		t.Errorf("description(00) = %q, want the official approved description", got)
	}
	if got := ResponseCodeDescription(ResponseCodeInsufficientFunds); got != "NO APROBADA-INSUF.DE FONDOS" {
		t.Errorf("description(51) = %q, want the official insufficient funds description", got)
	}
	if got := ResponseCodeDescription("XX"); got != "" {
		t.Errorf("description(XX) = %q, want empty string for unknown codes", got)
	}
}

func TestConfirmHandlerConcurrency(t *testing.T) {
	c := mustClient(t)
	handler := c.ConfirmHandler(func(*Confirmation) error { return nil })

	const n = 10
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(id int) {
			body := confirmBody(t, strconv.Itoa(id), "100.00", "PYG")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/confirm", bytes.NewReader([]byte(body)))
			handler(rec, req)
			done <- rec.Code
		}(i)
	}
	for i := 0; i < n; i++ {
		if code := <-done; code != http.StatusOK {
			t.Errorf("concurrent request status = %d, want 200", code)
		}
	}
}
