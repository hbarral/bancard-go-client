package bancard

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Checkout iframe integration helpers. After a successful SingleBuy, the
// web app needs two things to embed Bancard's PCI-compliant payment form:
// the checkout library script URL and the JavaScript snippet that
// initializes the form with the returned process_id. The web app owns
// rendering; these helpers only produce URLs and JavaScript strings.

const (
	checkoutScriptVersion = "4.0.0"
	zimpleScriptVersion   = "3.0.0"
)

// CheckoutScriptURL returns the URL of the Bancard checkout JavaScript
// library for the occasional payment (and card registration) iframe. The
// version differs per flow: occasional payment uses 4.0.0, Zimple 3.0.0.
func CheckoutScriptURL(env Environment) string {
	return string(env) + "/checkout/javascript/dist/bancard-checkout-" + checkoutScriptVersion + ".js"
}

// ZimpleScriptURL returns the URL of the Bancard checkout JavaScript
// library for the Zimple payment iframe.
func ZimpleScriptURL(env Environment) string {
	return string(env) + "/checkout/javascript/dist/bancard-checkout-" + zimpleScriptVersion + ".js"
}

// CheckoutFormSnippet returns the JavaScript that initializes the
// occasional payment iframe inside the element with the given container
// ID, e.g.:
//
//	Bancard.Checkout.createForm('iframe-container', '<processID>', {...});
//
// stylesJSON is optional styling in the format documented by Bancard
// (e.g. {"form-background-color": "#001b60", ...}); when empty or not
// valid JSON, it is omitted and the styles configured in the merchant
// portal apply.
func CheckoutFormSnippet(processID, containerID, stylesJSON string) string {
	return checkoutFormSnippet("Bancard.Checkout", processID, containerID, stylesJSON)
}

// ZimpleFormSnippet returns the JavaScript that initializes the Zimple
// payment iframe, e.g.:
//
//	Bancard.Zimple.createForm('iframe-container', '<processID>', {...});
//
// See CheckoutFormSnippet for the stylesJSON semantics.
func ZimpleFormSnippet(processID, containerID, stylesJSON string) string {
	return checkoutFormSnippet("Bancard.Zimple", processID, containerID, stylesJSON)
}

// checkoutFormSnippet builds the createForm call for the given Bancard
// namespace. Identifiers are escaped so quotes or backslashes cannot
// break out of the JavaScript string literals.
func checkoutFormSnippet(namespace, processID, containerID, stylesJSON string) string {
	args := []string{
		"'" + escapeJSString(containerID) + "'",
		"'" + escapeJSString(processID) + "'",
	}
	if styles := strings.TrimSpace(stylesJSON); styles != "" && json.Valid([]byte(styles)) {
		args = append(args, styles)
	}
	return fmt.Sprintf("%s.createForm(%s);", namespace, strings.Join(args, ", "))
}

// escapeJSString escapes backslashes and single quotes so the value is
// safe inside a single-quoted JavaScript string literal.
func escapeJSString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return r.Replace(s)
}
