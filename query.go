package bancard

import (
	"context"
	"fmt"
)

// get_single_buy_confirmation: queries whether a confirmation exists for
// a purchase. Use it when the single_buy_confirm webhook was not
// received; the recommended wait before querying is 10 minutes.

const singleBuyConfirmationsPath = "/vpos/api/0.3/single_buy/confirmations"

// ConfirmationStatus is the response of GetConfirmation.
type ConfirmationStatus struct {
	// Status is "success" or "error".
	Status string
	// Confirmation is the confirmation data when one exists for the
	// shop_process_id; nil otherwise.
	Confirmation *Confirmation
	// Messages carries additional operation messages, when present.
	Messages []APIMessage
}

// shopProcessIDOperation is the wire form shared by operations whose only
// parameters are the token and the shop_process_id.
type shopProcessIDOperation struct {
	Token         string `json:"token"`
	ShopProcessID int64  `json:"shop_process_id"`
}

// confirmationStatusAPIResponse is the wire form of the query response.
// The confirmation object uses the same shape as the single_buy_confirm
// callback payload.
type confirmationStatusAPIResponse struct {
	Status       string            `json:"status"`
	Confirmation *confirmOperation `json:"confirmation"`
	Messages     []APIMessage      `json:"messages"`
}

// GetConfirmation queries VPOS for the confirmation of a purchase
// identified by shop_process_id. It is the recovery path when the
// single_buy_confirm webhook was not received: if no confirmation is
// found, the returned ConfirmationStatus has a nil Confirmation and the
// merchant can then decide to roll back with Rollback.
func (c *Client) GetConfirmation(ctx context.Context, shopProcessID int64) (*ConfirmationStatus, error) {
	if shopProcessID <= 0 {
		return nil, fmt.Errorf("bancard: shop_process_id must be a positive integer")
	}

	payload := struct {
		PublicKey string                 `json:"public_key"`
		Operation shopProcessIDOperation `json:"operation"`
	}{
		PublicKey: c.publicKey,
		Operation: shopProcessIDOperation{
			Token:         c.getConfirmationToken(shopProcessID),
			ShopProcessID: shopProcessID,
		},
	}

	var resp confirmationStatusAPIResponse
	if err := c.doPost(ctx, singleBuyConfirmationsPath, payload, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, newAPIError(resp.Status, resp.Messages)
	}

	status := &ConfirmationStatus{
		Status:   resp.Status,
		Messages: resp.Messages,
	}
	if resp.Confirmation != nil {
		status.Confirmation = resp.Confirmation.confirmation()
	}
	return status, nil
}
