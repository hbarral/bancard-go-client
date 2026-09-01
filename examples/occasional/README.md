# Occasional Payment example

A runnable web app demonstrating the complete Occasional Payment flow with
the `bancard` package:

1. **Checkout** — the app calls `SingleBuy` and renders Bancard's checkout
   iframe with the returned `process_id`. Card data is entered inside
   Bancard's PCI-compliant iframe and never touches this app.
2. **Confirmation webhook** — Bancard POSTs the transaction result to
   `/bancard/confirm` (`ConfirmHandler`), which verifies the token and
   stores the confirmation.
3. **Voucher page** — `/finish` shows the user the allowed voucher fields
   (date/time, order number, amount, response description).
4. **Recovery** — `/status` queries VPOS with `GetConfirmation` when the
   webhook never arrived (VPOS recommends waiting ~10 minutes).
5. **Rollback** — `/rollback` reverses the transaction, distinguishing
   "already couponed" (manual reversal needed) from success.

## Running

Get your staging public/private keys from the merchant portal
(https://comercios.bancard.com.py), then:

```sh
go run ./examples/occasional \
  -public-key YOUR_PUBLIC_KEY \
  -private-key YOUR_PRIVATE_KEY \
  -addr localhost:8080 \
  -base-url https://YOUR-PUBLIC-HOST
```

Keys can also be provided via the `BANCARD_PUBLIC_KEY` and
`BANCARD_PRIVATE_KEY` environment variables. The environment is staging by
default; pass `-production` for production.

## Two things you must configure in the merchant portal

- The **confirmation URL** must be set to `{base-url}/bancard/confirm`.
  It cannot be passed per request, and Bancard requires TLS 1.2 on your
  endpoint.
- Your merchant **logo and form styles** (optional).

## Reaching the webhook from staging

VPOS must be able to POST to your confirmation URL. When developing
locally, expose the app through a tunnel (e.g. `ngrok http 8080`) and pass
the public URL with `-base-url`; otherwise the webhook cannot reach you and
you must recover confirmations with the `/status` query page.

## Staging test data

| Item | Value |
|------|-------|
| Visa test card | `4907860500000016`, exp `8/26`, CVV `570` |
| MasterCard test card | `5418630110000014`, exp `8/26`, CVV `277` |
| Bancard test card | `8601010000000013`, exp `8/26`, CVV N/D |
| Zimple test phone / OTP | `0981123456` / `1234` |

The demo charges a fixed Gs. 10330.00 order. After paying with a test card,
the confirmation webhook flips the order to approved on the voucher page
(remember to open the app through the tunnel URL so the iframe and redirect
flow work end to end).
