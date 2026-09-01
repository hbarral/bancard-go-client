package bancard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// single_buy_confirm: the transaction confirmation service offered by the
// merchant. At the end of a transaction VPOS POSTs the confirmation to
// the URL configured in the merchant portal, and the merchant must reply
// HTTP 200 with {"status":"success"} within 30 seconds or the
// confirmation is marked invalid. If the confirmation never arrives, the
// merchant can recover via GetConfirmation (recommended wait: 10
// minutes) or Rollback.

// Common transaction response codes; see ResponseCodeDescription for the
// full table.
const (
	ResponseCodeApproved          = "00"
	ResponseCodeCardDisabled      = "05"
	ResponseCodeInvalidTx         = "12"
	ResponseCodeInvalidCard       = "15"
	ResponseCodeInsufficientFunds = "51"
)

// SecurityInformation carries the security data attached to a
// confirmation.
type SecurityInformation struct {
	// CardSource is "L" (local) or "I" (international).
	CardSource string
	// CustomerIP is the customer's IP address.
	CustomerIP string
	// CardCountry is the card's country of origin.
	CardCountry string
	// Version is the API version used.
	Version string
	// RiskIndex is the real-time risk indicator: 0 low, 1 medium,
	// 2 high. For high risk, verify the transaction with the client and
	// contact riesgos@bancard.com.py if merchandise delivery is required.
	RiskIndex int
}

// BillingResponse reports the electronic invoice generation result. A
// failed invoice generation does not undo the payment: the failure is
// described here while the transaction remains processed.
type BillingResponse struct {
	Status      string         `json:"status"`
	Description string         `json:"description"`
	Data        map[string]any `json:"data"` // e.g. {"invoice_number": "001-001-0002563"}
}

// Confirmation is a transaction confirmation received from VPOS. The
// token is verified by ConfirmHandler before the value is handed to the
// application, so a received Confirmation is authentic.
type Confirmation struct {
	// ShopProcessID is the merchant's purchase identifier; it is the
	// idempotency key. VPOS may deliver the same confirmation more than
	// once, so handle it idempotently.
	ShopProcessID int64
	// Response is "S" when the process succeeded and "N" otherwise.
	Response string
	// ResponseDetails describes the process (max 60 characters).
	ResponseDetails string
	// Amount is the transaction amount in canonical string form.
	Amount string
	// IvaAmount is the IVA amount, when applicable.
	IvaAmount string
	// Currency is the transaction currency, e.g. "PYG".
	Currency string
	// AuthorizationNumber is the authorization code, present only on
	// approved transactions. Do not display it to the user.
	AuthorizationNumber string
	// TicketNumber is the authorization identifier.
	TicketNumber string
	// ResponseCode is the transaction response code (e.g. "00"); see
	// ResponseCodeDescription. Do not display it to the user.
	ResponseCode string
	// ResponseDescription is the response description, suitable for the
	// user-facing voucher.
	ResponseDescription string
	// ExtendedResponseDescription is the extended description. Do not
	// display it to the user.
	ExtendedResponseDescription string
	// Security carries the security information of the transaction. Do
	// not display it to the user.
	Security SecurityInformation
	// Billing reports the electronic invoice generation result, when the
	// request carried billing data.
	Billing *BillingResponse
}

// Approved reports whether the transaction was approved: response "S"
// with response code "00" (transacción aprobada).
func (cf *Confirmation) Approved() bool {
	return cf.Response == "S" && cf.ResponseCode == ResponseCodeApproved
}

// flexInt64 decodes a JSON number or a numeric string into an int64. The
// VPOS specification is inconsistent about the JSON type of some fields
// (shop_process_id and ticket_number appear both as integers and as
// strings across examples), so both forms are accepted.
type flexInt64 int64

// UnmarshalJSON implements json.Unmarshaler.
func (f *flexInt64) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	switch {
	case s == "" || s == "null":
		*f = 0
		return nil
	case len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"':
		s = s[1 : len(s)-1]
		if s == "" {
			*f = 0
			return nil
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("flexInt64: %q is neither a number nor a numeric string", string(data))
	}
	*f = flexInt64(v)
	return nil
}

// confirmPayload is the wire form of the confirmation request body.
type confirmPayload struct {
	Operation confirmOperation `json:"operation"`
}

type confirmOperation struct {
	Token                       string           `json:"token"`
	ShopProcessID               flexInt64        `json:"shop_process_id"`
	Response                    string           `json:"response"`
	ResponseDetails             string           `json:"response_details"`
	Amount                      string           `json:"amount"`
	IvaAmount                   string           `json:"iva_amount"`
	Currency                    string           `json:"currency"`
	AuthorizationNumber         string           `json:"authorization_number"`
	TicketNumber                flexInt64        `json:"ticket_number"`
	ResponseCode                string           `json:"response_code"`
	ResponseDescription         string           `json:"response_description"`
	ExtendedResponseDescription string           `json:"extended_response_description"`
	SecurityInformation         securityWire     `json:"security_information"`
	BillingResponse             *BillingResponse `json:"billing_response"`
}

type securityWire struct {
	CardSource  string    `json:"card_source"`
	CustomerIP  string    `json:"customer_ip"`
	CardCountry string    `json:"card_country"`
	Version     string    `json:"version"`
	RiskIndex   flexInt64 `json:"risk_index"`
}

// confirmation converts the wire form into the public Confirmation.
func (op *confirmOperation) confirmation() *Confirmation {
	return &Confirmation{
		ShopProcessID:               int64(op.ShopProcessID),
		Response:                    op.Response,
		ResponseDetails:             op.ResponseDetails,
		Amount:                      op.Amount,
		IvaAmount:                   op.IvaAmount,
		Currency:                    op.Currency,
		AuthorizationNumber:         op.AuthorizationNumber,
		TicketNumber:                strconv.FormatInt(int64(op.TicketNumber), 10),
		ResponseCode:                op.ResponseCode,
		ResponseDescription:         op.ResponseDescription,
		ExtendedResponseDescription: op.ExtendedResponseDescription,
		Security: SecurityInformation{
			CardSource:  op.SecurityInformation.CardSource,
			CustomerIP:  op.SecurityInformation.CustomerIP,
			CardCountry: op.SecurityInformation.CardCountry,
			Version:     op.SecurityInformation.Version,
			RiskIndex:   int(op.SecurityInformation.RiskIndex),
		},
		Billing: op.BillingResponse,
	}
}

// ConfirmHandler returns an http.HandlerFunc implementing the
// single_buy_confirm service: it receives VPOS's transaction confirmation
// POST, verifies the MD5 token, hands the Confirmation to onConfirm, and
// replies {"status":"success"} with HTTP 200 on success.
//
// The handler must complete within 30 seconds or VPOS marks the
// confirmation as invalid, so onConfirm must only do fast, synchronous
// work (persist the confirmation or enqueue it); slow work (emails,
// fulfillment) must run after replying. VPOS may redeliver
// confirmations, so onConfirm must treat shop_process_id idempotently.
//
// Response behavior:
//
//	200 {"status":"success"} — confirmation accepted
//	400 {"status":"error"}   — malformed JSON body
//	403 {"status":"error"}   — token verification failed (possible forgery)
//	405 {"status":"error"}   — method other than POST
//	500 {"status":"error"}   — onConfirm failed; recover later via
//	                           GetConfirmation or Rollback
func (c *Client) ConfirmHandler(onConfirm func(*Confirmation) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(status int, v any) {
			body, err := json.Marshal(v)
			if err != nil {
				http.Error(w, `{"status":"error"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(body)
		}

		if r.Method != http.MethodPost {
			writeJSON(http.StatusMethodNotAllowed, map[string]string{"status": "error"})
			return
		}

		var payload confirmPayload
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&payload); err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"status": "error"})
			return
		}
		op := payload.Operation

		// Token verification is the only defense against forged
		// confirmations; it must run before anything is trusted.
		if !c.verifyConfirmToken(op.Token, int64(op.ShopProcessID), op.Amount, op.Currency) {
			writeJSON(http.StatusForbidden, map[string]string{"status": "error"})
			return
		}

		confirmation := op.confirmation()
		if err := onConfirm(confirmation); err != nil {
			writeJSON(http.StatusInternalServerError, map[string]string{"status": "error"})
			return
		}

		writeJSON(http.StatusOK, map[string]string{"status": "success"})
	}
}
