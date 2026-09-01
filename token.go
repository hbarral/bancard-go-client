package bancard

import (
	"crypto/md5"
	"encoding/hex"
	"strconv"
)

// Token generation for the VPOS API. Every request carries an MD5 token
// built from the private key plus operation-specific parts, concatenated
// in exactly the order mandated by the API specification. The token is
// always 32 lowercase hex characters.
//
// Amounts participating in tokens must use the canonical two-decimal,
// dot-separated string form (see formatAmount), and the rollback token
// always uses the literal "0.00" regardless of the original transaction
// amount.

// rollbackAmount is the fixed amount used in the single_buy_rollback
// token formula.
const rollbackAmount = "0.00"

// md5Token returns the lowercase hex MD5 of the concatenated parts.
func md5Token(parts ...string) string {
	h := md5.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// singleBuyToken builds the token for the single_buy operation:
// md5(private_key + shop_process_id + amount + currency).
func (c *Client) singleBuyToken(shopProcessID int64, amount, currency string) string {
	return md5Token(c.privateKey, strconv.FormatInt(shopProcessID, 10), amount, currency)
}

// confirmToken builds the token used by VPOS in the single_buy_confirm
// callback, which the merchant must verify:
// md5(private_key + shop_process_id + "confirm" + amount + currency).
func (c *Client) confirmToken(shopProcessID int64, amount, currency string) string {
	return md5Token(c.privateKey, strconv.FormatInt(shopProcessID, 10), "confirm", amount, currency)
}

// getConfirmationToken builds the token for get_single_buy_confirmation:
// md5(private_key + shop_process_id + "get_confirmation").
func (c *Client) getConfirmationToken(shopProcessID int64) string {
	return md5Token(c.privateKey, strconv.FormatInt(shopProcessID, 10), "get_confirmation")
}

// rollbackToken builds the token for single_buy_rollback:
// md5(private_key + shop_process_id + "rollback" + "0.00").
func (c *Client) rollbackToken(shopProcessID int64) string {
	return md5Token(c.privateKey, strconv.FormatInt(shopProcessID, 10), "rollback", rollbackAmount)
}

// verifyConfirmToken reports whether the token carried by an incoming
// single_buy_confirm request is valid for the given fields. It is the
// only defense against forged confirmations, so every confirmation
// webhook must call it before trusting the payload.
func (c *Client) verifyConfirmToken(token string, shopProcessID int64, amount, currency string) bool {
	return token == c.confirmToken(shopProcessID, amount, currency)
}
