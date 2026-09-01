package bancard

import (
	"context"
	"fmt"
)

// single_buy_rollback: reverses a transaction. Per the VPOS
// specification, a rollback can only be sent on the same day as the
// transaction (before it appears on the customer's statement) and
// succeeds as long as the transaction has not been "couponed".

const singleBuyRollbackPath = "/vpos/api/0.3/single_buy/rollback"

// Rollback reverses the purchase identified by shop_process_id. Typical
// use cases are payment reversals, transactions canceled by the user on
// the VPOS page, abandoned or incomplete transactions, and cancellation
// of preauthorizations (which otherwise expire after 30 days).
//
// Return value semantics:
//
//	nil — the rollback succeeded, or VPOS answered PaymentNotFoundError
//	      (no payment existed for the shop_process_id, which the
//	      specification mandates treating as success).
//
//	*APIError with key TransactionAlreadyConfirmed — the transaction was
//	      couponed (confirmed on the customer's statement) and cannot be
//	      reversed automatically; it must be processed manually through
//	      Bancard's Commercial Area.
//
//	*APIError with key AlreadyRollbackedError — a rollback already
//	      exists; the goal state is already reached, but the caller is
//	      informed rather than the fact being silently dropped.
//
//	other non-nil error — transport failure or any other VPOS error.
func (c *Client) Rollback(ctx context.Context, shopProcessID int64) error {
	if shopProcessID <= 0 {
		return fmt.Errorf("bancard: shop_process_id must be a positive integer")
	}

	// The rollback token always uses the fixed amount "0.00" regardless
	// of the original transaction amount; see rollbackToken.
	payload := struct {
		PublicKey string `json:"public_key"`
		Operation struct {
			Token         string `json:"token"`
			ShopProcessID string `json:"shop_process_id"`
		} `json:"operation"`
	}{
		PublicKey: c.publicKey,
	}
	payload.Operation.Token = c.rollbackToken(shopProcessID)
	payload.Operation.ShopProcessID = fmt.Sprintf("%d", shopProcessID)

	var resp struct {
		Status   string       `json:"status"`
		Messages []APIMessage `json:"messages"`
	}
	if err := c.doPost(ctx, singleBuyRollbackPath, payload, &resp); err != nil {
		return err
	}

	if resp.Status == "success" {
		return nil
	}

	apiErr := newAPIError(resp.Status, resp.Messages)

	// The specification states that PaymentNotFoundError on rollback must
	// be treated as success: no payment existed to reverse.
	if apiErr.Has(MessageKeyPaymentNotFound) {
		return nil
	}

	// TransactionAlreadyConfirmed and AlreadyRollbackedError are returned
	// as the typed *APIError so the caller can branch on the message keys
	// without losing the full response.
	return apiErr
}
