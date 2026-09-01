package bancard

import (
	"fmt"
	"strings"
)

// Well-known VPOS message keys returned in API responses. These are the
// keys with special semantics for the occasional payment flow; see the
// VPOS specification for the full list.
const (
	// MessageKeyRollbackSuccessful indicates a successful rollback.
	MessageKeyRollbackSuccessful = "RollbackSuccessful"
	// MessageKeyInvalidToken indicates incorrect token generation.
	MessageKeyInvalidToken = "InvalidTokenError"
	// MessageKeyPaymentNotFound indicates no payment exists for the
	// shop_process_id. On rollback it must be treated as success.
	MessageKeyPaymentNotFound = "PaymentNotFoundError"
	// MessageKeyAlreadyRollbacked indicates a previous rollback exists.
	MessageKeyAlreadyRollbacked = "AlreadyRollbackedError"
	// MessageKeyTransactionAlreadyConfirmed indicates the transaction was
	// couponed (confirmed on the customer's statement) and cannot be
	// rolled back automatically; reversal must be processed manually
	// through Bancard's Commercial Area.
	MessageKeyTransactionAlreadyConfirmed = "TransactionAlreadyConfirmed"
)

// APIMessage is a single message returned by VPOS in operation responses.
type APIMessage struct {
	Key   string `json:"key"`
	Level string `json:"level"` // "info" or "error"
	Dsc   string `json:"dsc"`
}

// APIError is returned by client calls when VPOS responds with a status
// other than "success". It carries the raw messages so callers can branch
// on specific message keys.
type APIError struct {
	Status   string       `json:"status"` // "error"
	Messages []APIMessage `json:"messages"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if len(e.Messages) == 0 {
		return fmt.Sprintf("bancard: API error (status %q)", e.Status)
	}
	parts := make([]string, len(e.Messages))
	for i, m := range e.Messages {
		parts[i] = m.Key + ": " + m.Dsc
	}
	return "bancard: " + strings.Join(parts, "; ")
}

// Has reports whether the response contains a message with the given key.
func (e *APIError) Has(key string) bool {
	for _, m := range e.Messages {
		if m.Key == key {
			return true
		}
	}
	return false
}

// newAPIError builds an APIError from a non-success response.
func newAPIError(status string, messages []APIMessage) *APIError {
	return &APIError{Status: status, Messages: messages}
}
