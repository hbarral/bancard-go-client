# bancard-go-client

A Go client for the [Bancard VPOS 2.0](https://vpos.infonet.com.py) eCommerce
payment gateway. It implements the **Occasional Payment** flow: initiate a
`single_buy` order, render the PCI-compliant checkout iframe, verify incoming
transaction confirmations, query confirmation status, and roll back
transactions.

Card data **never** passes through this package — it is entered by the user
directly inside Bancard's PCI-compliant iframe. The package only handles the
API calls and the checkout iframe plumbing.

## Features

- `single_buy` payment order creation (`SingleBuy`)
- Checkout iframe helpers (script URL + JavaScript snippet)
- Transaction confirmation webhook handler with MD5 token verification
  (`ConfirmHandler`)
- Confirmation status recovery query (`GetConfirmation`)
- Transaction rollback with typed error semantics (`Rollback`)
- Local request validation before any network call
- Optional Zimple payments and preauthorizations
- Optional electronic invoicing (`Billing`)
- Transaction response-code descriptions in Spanish (`ResponseCodeDescription`)

## Installation

```sh
go get github.com/hbarral/bancard-go-client
```

Requires Go 1.27+.

## Getting started

Get your public/private keys from the merchant portal at
<https://comercios.bancard.com.py>.

```go
package main

import (
	"context"
	"fmt"
	"log"

	bancard "github.com/hbarral/bancard-go-client"
)

func main() {
	client, err := bancard.New(
		"YOUR_PUBLIC_KEY",   // 32 alphanumeric chars
		"YOUR_PRIVATE_KEY",  // 40 printable ASCII chars ([!-~])
		bancard.Staging,     // or bancard.Production
	)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.SingleBuy(context.Background(), &bancard.SingleBuyRequest{
		ShopProcessID: 123456,
		Amount:        "10330.00", // canonical decimal, see "Amounts" below
		Currency:      "PYG",
		Description:   "Compra de prueba", // max 20 characters
		ReturnURL:     "https://your-app.example.com/finish",
		CancelURL:     "https://your-app.example.com/cancel",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("process_id:", resp.ProcessID)
}
```

### Environments

| Constant           | URL                             |
|--------------------|---------------------------------|
| `bancard.Staging`  | `https://vpos.infonet.com.py:8888` |
| `bancard.Production` | `https://vpos.infonet.com.py`  |

The client targets VPOS API version `0.3`.

### Options

`New` accepts optional configuration:

```go
client, err := bancard.New(
	publicKey, privateKey, bancard.Production,
	bancard.WithHTTPClient(&http.Client{Timeout: 45 * time.Second}),
	bancard.WithUserAgent("my-webapp/1.0"),
)
```

A `Client` is immutable and safe for concurrent use: create one per
application and reuse it.

## Rendering the checkout iframe

After a successful `SingleBuy`, embed Bancard's checkout script and initialize
the form with the returned `process_id`:

```go
scriptURL := bancard.CheckoutScriptURL(bancard.Staging)
snippet   := bancard.CheckoutFormSnippet(resp.ProcessID, "iframe-container", "")
```

Then in your page:

```html
<div id="iframe-container"></div>
<script src="{{scriptURL}}"></script>
<script>
  window.onload = function() { {{snippet}} };
</script>
```

`CheckoutFormSnippet` produces a call like
`Bancard.Checkout.createForm('iframe-container', '<processID>', {...});`. The
third argument is optional JSON styling; when empty the styles configured in
the merchant portal apply.

For Zimple payments, use `ZimpleScriptURL` and `ZimpleFormSnippet` instead,
and set `Zimple: true` plus the user's phone number in `AdditionalData` on the
request.

## Handling the confirmation webhook

Bancard POSTs the transaction result to your configured **confirmation URL**
(configured in the merchant portal, not per request). Use `ConfirmHandler` to
verify the MD5 token and deliver the confirmation to your code:

```go
mux := http.NewServeMux()
mux.HandleFunc("POST /bancard/confirm",
	client.ConfirmHandler(func(cf *bancard.Confirmation) error {
		if cf.Approved() {
			// fulfill the order for cf.ShopProcessID
		} else {
			// mark as denied
		}
		return nil // nil replies {"status":"success"} with HTTP 200
	}))
```

Rules to keep in mind:

- The handler must reply within **30 seconds**, so `onConfirm` must only do
  fast, synchronous work (persist or enqueue). Slow work must run after
  replying.
- VPOS may redeliver the same confirmation, so treat `ShopProcessID`
  idempotently.
- Token verification is the only defense against forged confirmations; the
  handler performs it before calling your callback.
- Do **not** display `AuthorizationNumber`, `ResponseCode`,
  `ExtendedResponseDescription`, or `Security` to the user.

## Recovering a missing confirmation

If the webhook never arrives, query VPOS for the confirmation status
(VPOS recommends waiting ~10 minutes before querying):

```go
status, err := client.GetConfirmation(context.Background(), 123456)
if err != nil {
	log.Fatal(err)
}
if status.Confirmation != nil {
	fmt.Println("approved:", status.Confirmation.Approved())
} else {
	// no confirmation found; consider Rollback
}
```

## Rolling back a transaction

Reverses the purchase identified by `shop_process_id`. Rollbacks must be sent
on the same day as the transaction (before it appears on the customer's
statement).

```go
err := client.Rollback(context.Background(), 123456)
```

Return-value semantics:

| Return value | Meaning |
|--------------|---------|
| `nil` | Rollback succeeded, or VPOS answered `PaymentNotFoundError` (treated as success). |
| `*APIError` with `TransactionAlreadyConfirmed` | Transaction was "couponed" and cannot be reversed automatically; process manually through Bancard's Commercial Area. |
| `*APIError` with `AlreadyRollbackedError` | A rollback already exists; the goal state is already reached. |
| other error | Transport failure or any other VPOS error. |

## Amounts

Amounts are **canonical decimal `(15,2)` strings**: digits, a dot, and exactly
two decimal digits. For example `10330.00` (Gs 10,330). This format is also
what the MD5 token formula requires. Use `strconv.FormatFloat(v, 'f', 2, 64)`
to produce it from a `float64`.

`PYG` (Guaraníes) is the only currency supported by VPOS today.

## Error handling

Client methods return a typed `*bancard.APIError` when VPOS responds with a
status other than `success`. Branch on specific message keys with `Has`:

```go
var apiErr *bancard.APIError
if errors.As(err, &apiErr) {
	switch {
	case apiErr.Has(bancard.MessageKeyTransactionAlreadyConfirmed):
		// manual reversal required
	case apiErr.Has(bancard.MessageKeyAlreadyRollbacked):
		// already rolled back
	case apiErr.Has(bancard.MessageKeyInvalidToken):
		// token generation is incorrect — check your keys/format
	}
}
```

Construction errors (`bancard.New`) return `ErrInvalidPublicKey`,
`ErrInvalidPrivateKey`, or `ErrInvalidEnvironment`, so misconfiguration fails
fast instead of surfacing as `InvalidTokenError` responses from VPOS.

## Response codes

Transaction response codes (e.g. `"51"` = insufficient funds) are documented
via `ResponseCodeDescription`, which returns the official Spanish description:

```go
fmt.Println(bancard.ResponseCodeDescription("51")) // NO APROBADA-INSUF.DE FONDOS
```

`Confirmation.Approved()` returns `true` when the response is `"S"` and the
response code is `"00"`.

## Full example

A runnable end-to-end web app is provided in
[`examples/occasional`](examples/occasional), including staging test cards and
a walkthrough. Run it with:

```sh
go run ./examples/occasional \
  -public-key YOUR_PUBLIC_KEY \
  -private-key YOUR_PRIVATE_KEY \
  -base-url https://YOUR-PUBLIC-HOST
```

See [`examples/occasional/README.md`](examples/occasional/README.md) for the
full flow, the confirmation URL you must configure, and staging test data.

## License

See the repository license.
