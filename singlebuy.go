package bancard

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// singleBuyPath is the VPOS endpoint for the single_buy operation,
// including its Zimple and preauthorization variants.
const singleBuyPath = "/vpos/api/0.3/single_buy"

var (
	// amountPattern matches the canonical VPOS decimal (15,2) string
	// form: digits, dot, exactly two decimal digits.
	amountPattern   = regexp.MustCompile(`^\d+\.\d{2}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// BillingDetail is a line item of an electronic invoice (billing).
type BillingDetail struct {
	// Description is the item description (max 255 characters).
	Description string `json:"description"`
	// Amount is the unit price in canonical decimal (15,2) string form.
	Amount string `json:"amount"`
	// IvaRate is the IVA rate applied to the item.
	IvaRate int `json:"iva_rate"`
	// TotalItems is the quantity of items.
	TotalItems int `json:"total_items"`
}

// Billing carries electronic invoice data for a single_buy request. Only
// merchants enabled for Electronic Invoicing may send it; all invoices
// are also available in the electronic invoicing portal.
type Billing struct {
	// ClientRuc is the client RUC. Empty means an unnamed invoice, which
	// can only be generated up to Gs 7,000,000 (DNIT regulation).
	ClientRuc string `json:"client_ruc,omitempty"`
	// ClientName is required when ClientRuc is set (max 100 characters).
	ClientName string `json:"client_name,omitempty"`
	// ClientEmail is required when ClientRuc is set (max 32 characters).
	ClientEmail string `json:"client_email,omitempty"`
	// CommerceStamp is the stamping number (max 32 characters).
	CommerceStamp string `json:"commerce_stamp"`
	// CommerceExpeditionPoint is the issuance point (max 32 characters).
	CommerceExpeditionPoint string `json:"commerce_expedition_point"`
	// CommerceEstablishment is the establishment (max 32 characters). It
	// and the expedition point must correspond to CommerceStamp.
	CommerceEstablishment string `json:"commerce_establishment"`
	// Details is the item detail list; it cannot be empty and its total
	// cost must match the operation amount.
	Details []BillingDetail `json:"details"`
}

// SingleBuyRequest initiates an occasional payment order. Amounts are
// canonical decimal (15,2) strings such as "10330.00" (two decimal
// digits, dot separator), matching the token formula requirements.
type SingleBuyRequest struct {
	// ShopProcessID is the merchant's unique purchase identifier
	// (Integer 15). It doubles as the idempotency key across the whole
	// flow: confirmations, queries and rollbacks reference it.
	ShopProcessID int64
	// Amount is the amount in Guaranies, e.g. "10330.00". Must be
	// greater than zero.
	Amount string
	// Currency is the currency type, 3 uppercase letters. PYG (Gs) is
	// the only value supported by VPOS today.
	Currency string
	// Description is the payment description shown to the user
	// (required, max 20 characters).
	Description string
	// ReturnURL is the URL the user is redirected to after payment
	// (required, max 255 characters).
	ReturnURL string
	// CancelURL is the URL for payment cancellation; it defaults to
	// ReturnURL when empty (max 255 characters).
	CancelURL string
	// IvaAmount is the IVA amount, for merchants under the Digital
	// Services Law. Optional; canonical amount format when set.
	IvaAmount string
	// AdditionalData is an optional field (max 255 characters) used for
	// promotions (traditional or BIN format), or for the user's Zimple
	// phone number when Zimple is true.
	AdditionalData string
	// Preauthorization marks the transaction as a preauthorization
	// ("S" on the wire) when true.
	Preauthorization bool
	// Zimple selects the Zimple variant ("S" on the wire) when true;
	// AdditionalData must then contain the user's Zimple phone number.
	Zimple bool
	// ExtraResponseAttributes requests extra data in responses, e.g.
	// "payment_card_type" (returns "credit" or "debit").
	ExtraResponseAttributes []string
	// Billing carries optional electronic invoice data.
	Billing *Billing
}

// SingleBuyResponse is the result of a successful single_buy operation.
type SingleBuyResponse struct {
	// Status is the response status, "success" on success.
	Status string
	// ProcessID identifies the payment process; it is used to invoke the
	// Bancard checkout iframe (see CheckoutFormSnippet).
	ProcessID string
}

// singleBuyOperation is the wire form of the operation element.
type singleBuyOperation struct {
	Token                   string   `json:"token"`
	ShopProcessID           int64    `json:"shop_process_id"`
	Amount                  string   `json:"amount"`
	IvaAmount               string   `json:"iva_amount,omitempty"`
	Currency                string   `json:"currency"`
	AdditionalData          string   `json:"additional_data,omitempty"`
	Preauthorization        string   `json:"preauthorization,omitempty"`
	Description             string   `json:"description"`
	ReturnURL               string   `json:"return_url"`
	CancelURL               string   `json:"cancel_url,omitempty"`
	Simple                  string   `json:"simple,omitempty"`
	ExtraResponseAttributes []string `json:"extra_response_attributes,omitempty"`
	Billing                 *Billing `json:"billing,omitempty"`
}

// singleBuyAPIResponse is the wire form of the single_buy response.
type singleBuyAPIResponse struct {
	Status    string       `json:"status"`
	ProcessID string       `json:"process_id"`
	Messages  []APIMessage `json:"messages"`
}

// SingleBuy initiates an occasional payment order. On success it returns
// the process_id used to render the Bancard checkout iframe. The request
// is validated locally before any network call.
func (c *Client) SingleBuy(ctx context.Context, req *SingleBuyRequest) (*SingleBuyResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	op := singleBuyOperation{
		Token:                   c.singleBuyToken(req.ShopProcessID, req.Amount, req.Currency),
		ShopProcessID:           req.ShopProcessID,
		Amount:                  req.Amount,
		IvaAmount:               req.IvaAmount,
		Currency:                req.Currency,
		AdditionalData:          req.AdditionalData,
		Description:             req.Description,
		ReturnURL:               req.ReturnURL,
		CancelURL:               req.CancelURL,
		ExtraResponseAttributes: req.ExtraResponseAttributes,
		Billing:                 req.Billing,
	}
	if req.Preauthorization {
		op.Preauthorization = "S"
	}
	if req.Zimple {
		op.Simple = "S"
	}

	payload := struct {
		PublicKey string             `json:"public_key"`
		Operation singleBuyOperation `json:"operation"`
	}{
		PublicKey: c.publicKey,
		Operation: op,
	}

	var resp singleBuyAPIResponse
	if err := c.doPost(ctx, singleBuyPath, payload, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, newAPIError(resp.Status, resp.Messages)
	}
	if resp.ProcessID == "" {
		return nil, fmt.Errorf("bancard: single_buy succeeded but response has no process_id")
	}

	return &SingleBuyResponse{Status: resp.Status, ProcessID: resp.ProcessID}, nil
}

// validate checks the request against the field limits and business rules
// documented in the VPOS specification, so misconfiguration fails before
// hitting the API.
func (r *SingleBuyRequest) validate() error {
	var errs []string

	if r.ShopProcessID <= 0 {
		errs = append(errs, "shop_process_id must be a positive integer")
	}
	if !amountPattern.MatchString(r.Amount) {
		errs = append(errs, fmt.Sprintf("amount %q must match \\d+.\\d{2} (e.g. \"10330.00\")", r.Amount))
	} else if amount, err := strconv.ParseFloat(r.Amount, 64); err != nil || amount <= 0 {
		errs = append(errs, "amount must be greater than zero")
	}
	if !currencyPattern.MatchString(r.Currency) {
		errs = append(errs, fmt.Sprintf("currency %q must be 3 uppercase letters (PYG is the only value supported today)", r.Currency))
	}
	if r.Description == "" {
		errs = append(errs, "description is required")
	} else if len(r.Description) > 20 {
		errs = append(errs, fmt.Sprintf("description is %d characters, max is 20", len(r.Description)))
	}
	if r.ReturnURL == "" {
		errs = append(errs, "return_url is required")
	} else if len(r.ReturnURL) > 255 {
		errs = append(errs, fmt.Sprintf("return_url is %d characters, max is 255", len(r.ReturnURL)))
	}
	if len(r.CancelURL) > 255 {
		errs = append(errs, fmt.Sprintf("cancel_url is %d characters, max is 255", len(r.CancelURL)))
	}
	if len(r.AdditionalData) > 255 {
		errs = append(errs, fmt.Sprintf("additional_data is %d characters, max is 255", len(r.AdditionalData)))
	}
	if r.IvaAmount != "" && !amountPattern.MatchString(r.IvaAmount) {
		errs = append(errs, fmt.Sprintf("iva_amount %q must match \\d+.\\d{2}", r.IvaAmount))
	}
	if r.Zimple && r.AdditionalData == "" {
		errs = append(errs, "zimple requires additional_data with the user's Zimple phone number")
	}
	for _, attr := range r.ExtraResponseAttributes {
		if attr == "" {
			errs = append(errs, "extra_response_attributes cannot contain empty values")
			break
		}
	}
	if r.Billing != nil {
		if err := r.Billing.validate(r.Amount); err != nil {
			errs = append(errs, "billing: "+err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("bancard: invalid SingleBuyRequest: %s", strings.Join(errs, "; "))
	}
	return nil
}

// validate checks the billing element rules: commerce data is required,
// named invoices require client name and email, details cannot be empty,
// and the total cost of the details must match the operation amount.
func (b *Billing) validate(operationAmount string) error {
	var errs []string

	if b.ClientRuc != "" {
		if b.ClientName == "" {
			errs = append(errs, "client_name is required when client_ruc is set")
		}
		if b.ClientEmail == "" {
			errs = append(errs, "client_email is required when client_ruc is set")
		}
	}
	if b.CommerceStamp == "" {
		errs = append(errs, "commerce_stamp is required")
	}
	if b.CommerceExpeditionPoint == "" {
		errs = append(errs, "commerce_expedition_point is required")
	}
	if b.CommerceEstablishment == "" {
		errs = append(errs, "commerce_establishment is required")
	}
	if len(b.Details) == 0 {
		errs = append(errs, "details cannot be empty")
	}

	var total float64
	for i, d := range b.Details {
		if d.Description == "" {
			errs = append(errs, fmt.Sprintf("details[%d]: description is required", i))
		}
		if !amountPattern.MatchString(d.Amount) {
			errs = append(errs, fmt.Sprintf("details[%d]: amount %q must match \\d+.\\d{2}", i, d.Amount))
		} else {
			unit, err := strconv.ParseFloat(d.Amount, 64)
			if err != nil {
				errs = append(errs, fmt.Sprintf("details[%d]: invalid amount %q", i, d.Amount))
			} else {
				total += unit * float64(d.TotalItems)
			}
		}
		if d.TotalItems < 1 {
			errs = append(errs, fmt.Sprintf("details[%d]: total_items must be at least 1", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	opAmount, err := strconv.ParseFloat(operationAmount, 64)
	if err != nil {
		return fmt.Errorf("cannot compare details total with operation amount %q", operationAmount)
	}
	if math.Abs(total-opAmount) > 0.005 {
		return fmt.Errorf("details total %.2f must match operation amount %s", total, operationAmount)
	}
	return nil
}
