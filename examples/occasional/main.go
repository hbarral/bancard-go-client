// Command occasional is a runnable example of the full Occasional Payment
// flow using the bancard package: it initiates a single_buy payment,
// renders Bancard's checkout iframe, receives the transaction
// confirmation webhook, lets the user query the confirmation status, and
// can roll back the transaction.
//
// Run it with your staging keys (see README.md in this directory) and
// open http://localhost:8080.
package main

import (
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	bancard "github.com/hbarral/bancard-go-client"
)

// order is the merchant-side record of a purchase. A real application
// would persist this in a database.
type order struct {
	ID           int64
	Amount       string
	Description  string
	Status       string // pending | approved | denied | rolled_back
	ProcessID    string
	Confirmation *bancard.Confirmation
	CreatedAt    time.Time
}

// store is an in-memory, concurrency-safe order store.
type store struct {
	mu     sync.Mutex
	nextID int64
	orders map[int64]*order
}

func newStore() *store {
	return &store{nextID: 1000, orders: make(map[int64]*order)}
}

func (s *store) create(amount, description string) *order {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := &order{
		ID:          s.nextID,
		Amount:      amount,
		Description: description,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}
	s.nextID++
	s.orders[o.ID] = o
	return o
}

func (s *store) get(id int64) *order {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orders[id]
}

func (s *store) setProcessID(id int64, processID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := s.orders[id]; o != nil {
		o.ProcessID = processID
	}
}

// saveConfirmation is the ConfirmHandler callback. It must be fast
// (VPOS requires a 200 within 30 seconds) and idempotent: VPOS may
// redeliver the same confirmation.
func (s *store) saveConfirmation(cf *bancard.Confirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[cf.ShopProcessID]
	if o == nil {
		return fmt.Errorf("unknown shop_process_id %d", cf.ShopProcessID)
	}
	o.Confirmation = cf
	if cf.Approved() {
		o.Status = "approved"
	} else {
		o.Status = "denied"
	}
	return nil
}

func (s *store) markRolledBack(id int64, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := s.orders[id]; o != nil {
		o.Status = status
	}
}

var page = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Bancard occasional payment demo</title>
<style>
body { font-family: sans-serif; max-width: 640px; margin: 2rem auto; color: #111; }
table { border-collapse: collapse; } td, th { border: 1px solid #ccc; padding: .4rem .8rem; text-align: left; }
a.button, button { display: inline-block; padding: .5rem 1rem; background: #001b60; color: #fff; border: none; border-radius: 4px; text-decoration: none; cursor: pointer; }
.pending { color: #b8860b; } .approved { color: #060; } .denied, .error { color: #c00; }
</style>
</head>
<body>
<h1>Occasional payment demo</h1>
{{block "content" .}}{{end}}
</body>
</html>`))

var templates = template.Must(template.Must(page.Clone()).Parse(`
{{define "content"}}
<p>Create a payment order for Gs. {{.amount}}.</p>
<form method="POST" action="/checkout">
  <input type="hidden" name="amount" value="{{.amount}}">
  <button type="submit">Pay Gs. {{.amount}}</button>
</form>
{{end}}`))

var checkoutTpl = template.Must(template.Must(page.Clone()).Parse(`
{{define "content"}}
<p>Order <strong>{{.orderID}}</strong> for Gs. {{.amount}} is {{.status}}.</p>
<div id="iframe-container" style="max-width:420px">Loading payment form...</div>
<script src="{{.scriptURL}}"></script>
<script>
window.onload = function() {
	{{.snippet}}
};
</script>
<p><a href="/finish?order={{.orderID}}">I already paid / check status</a></p>
{{end}}`))

var finishTpl = template.Must(template.Must(page.Clone()).Parse(`
{{define "content"}}
<h2>Order {{.orderID}}: <span class="{{.statusClass}}">{{.status}}</span></h2>
{{if .responseDescription}}
<table>
<tr><th>Date and time</th><td>{{.confirmedAt}}</td></tr>
<tr><th>Order number</th><td>{{.orderID}}</td></tr>
<tr><th>Amount</th><td>{{.amount}}</td></tr>
<tr><th>Response</th><td>{{.responseDescription}}</td></tr>
</table>
{{if eq .status "approved"}}
<form method="POST" action="/rollback">
  <input type="hidden" name="order" value="{{.orderID}}">
  <button type="submit">Roll back transaction</button>
</form>
{{end}}
{{else}}
<p>No confirmation received yet for this order.</p>
<p><a class="button" href="/status?order={{.orderID}}">Query confirmation at VPOS</a></p>
{{end}}
<p><a href="/">Back to start</a></p>
{{end}}`))

var errorTpl = template.Must(template.Must(page.Clone()).Parse(`
{{define "content"}}
<p class="error">{{.}}</p>
<p><a href="/">Back to start</a></p>
{{end}}`))

type server struct {
	client *bancard.Client
	store  *store
	env    bancard.Environment
	// baseURL is this app's public URL; it is used for return_url and
	// cancel_url. The confirmation URL must be configured separately in
	// the merchant portal.
	baseURL string
}

func main() {
	var (
		addr       = flag.String("addr", "localhost:8080", "listen address")
		baseURL    = flag.String("base-url", "http://localhost:8080", "public base URL of this app")
		publicKey  = flag.String("public-key", os.Getenv("BANCARD_PUBLIC_KEY"), "Bancard public key")
		privateKey = flag.String("private-key", os.Getenv("BANCARD_PRIVATE_KEY"), "Bancard private key")
		production = flag.Bool("production", false, "use the production environment (default: staging)")
	)
	flag.Parse()

	env := bancard.Staging
	if *production {
		env = bancard.Production
	}

	client, err := bancard.New(*publicKey, *privateKey, env)
	if err != nil {
		log.Fatalf("invalid credentials or environment: %v", err)
	}

	srv := &server{client: client, store: newStore(), env: env, baseURL: strings.TrimSuffix(*baseURL, "/")}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", srv.handleIndex)
	mux.HandleFunc("POST /checkout", srv.handleCheckout)
	mux.HandleFunc("GET /finish", srv.handleFinish)
	mux.HandleFunc("GET /status", srv.handleStatus)
	mux.HandleFunc("POST /rollback", srv.handleRollback)

	// The single_buy_confirm webhook. Register this exact path
	// (/bancard/confirm) as the confirmation URL in the merchant portal.
	mux.Handle("POST /bancard/confirm", client.ConfirmHandler(srv.store.saveConfirmation))

	log.Printf("listening on http://%s (environment: %s)", *addr, env)
	log.Printf("configure %s/bancard/confirm as the confirmation URL in the merchant portal", srv.baseURL)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, templates, map[string]string{"amount": "10330.00"})
}

func (s *server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	amount := r.FormValue("amount")
	o := s.store.create(amount, "Ejemplo de pago")

	resp, err := s.client.SingleBuy(r.Context(), &bancard.SingleBuyRequest{
		ShopProcessID: o.ID,
		Amount:        o.Amount,
		Currency:      "PYG",
		Description:   o.Description, // max 20 characters
		ReturnURL:     s.baseURL + "/finish?order=" + strconv.FormatInt(o.ID, 10),
		CancelURL:     s.baseURL + "/finish?order=" + strconv.FormatInt(o.ID, 10),
	})
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.store.setProcessID(o.ID, resp.ProcessID)

	// The web app embeds Bancard's iframe; card data never touches this
	// application.
	s.render(w, checkoutTpl, map[string]string{
		"orderID":   strconv.FormatInt(o.ID, 10),
		"amount":    o.Amount,
		"status":    o.Status,
		"scriptURL": bancard.CheckoutScriptURL(s.env),
		"snippet":   bancard.CheckoutFormSnippet(resp.ProcessID, "iframe-container", ""),
	})
}

func (s *server) handleFinish(w http.ResponseWriter, r *http.Request) {
	o := s.orderFromRequest(w, r)
	if o == nil {
		return
	}
	s.renderFinish(w, o)
}

// handleStatus recovers via GetConfirmation when the webhook never
// arrived (the VPOS recommendation is to wait ~10 minutes first).
func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	o := s.orderFromRequest(w, r)
	if o == nil {
		return
	}

	status, err := s.client.GetConfirmation(r.Context(), o.ID)
	if err != nil {
		s.renderError(w, err)
		return
	}
	if status.Confirmation != nil {
		if err := s.store.saveConfirmation(status.Confirmation); err != nil {
			s.renderError(w, err)
			return
		}
		o = s.store.get(o.ID)
	}
	s.renderFinish(w, o)
}

func (s *server) handleRollback(w http.ResponseWriter, r *http.Request) {
	o := s.orderFromRequest(w, r)
	if o == nil {
		return
	}

	err := s.client.Rollback(r.Context(), o.ID)
	switch {
	case err == nil:
		s.store.markRolledBack(o.ID, "rolled_back")
	case isAPIErrorWith(err, bancard.MessageKeyTransactionAlreadyConfirmed):
		s.store.markRolledBack(o.ID, "rollback_manual")
	case isAPIErrorWith(err, bancard.MessageKeyAlreadyRollbacked):
		s.store.markRolledBack(o.ID, "rolled_back")
	default:
		s.renderError(w, err)
		return
	}
	s.renderFinish(w, s.store.get(o.ID))
}

func isAPIErrorWith(err error, key string) bool {
	var apiErr *bancard.APIError
	return errors.As(err, &apiErr) && apiErr.Has(key)
}

func (s *server) orderFromRequest(w http.ResponseWriter, r *http.Request) *order {
	id, err := strconv.ParseInt(r.FormValue("order"), 10, 64)
	if err != nil {
		s.renderError(w, fmt.Errorf("invalid order id"))
		return nil
	}
	o := s.store.get(id)
	if o == nil {
		s.renderError(w, fmt.Errorf("unknown order %d", id))
		return nil
	}
	return o
}

func (s *server) renderFinish(w http.ResponseWriter, o *order) {
	data := map[string]string{
		"orderID": strconv.FormatInt(o.ID, 10),
		"amount":  o.Amount,
		"status":  o.Status,
	}
	switch o.Status {
	case "pending":
		data["statusClass"] = "pending"
	case "approved":
		data["statusClass"] = "approved"
	default:
		data["statusClass"] = "denied"
	}
	if o.Confirmation != nil {
		data["confirmedAt"] = o.CreatedAt.Format("2006-01-02 15:04:05")
		data["responseDescription"] = o.Confirmation.ResponseDescription
	}
	s.render(w, finishTpl, data)
}

func (s *server) render(w http.ResponseWriter, tpl *template.Template, data map[string]string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.Execute(w, data); err != nil {
		log.Printf("template: %v", err)
	}
}

func (s *server) renderError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	s.render(w, errorTpl, map[string]string{"error": err.Error()})
}
