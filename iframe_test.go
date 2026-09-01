package bancard

import "testing"

func TestCheckoutScriptURL(t *testing.T) {
	tests := []struct {
		env  Environment
		want string
	}{
		{Staging, "https://vpos.infonet.com.py:8888/checkout/javascript/dist/bancard-checkout-4.0.0.js"},
		{Production, "https://vpos.infonet.com.py/checkout/javascript/dist/bancard-checkout-4.0.0.js"},
	}
	for _, tt := range tests {
		if got := CheckoutScriptURL(tt.env); got != tt.want {
			t.Errorf("CheckoutScriptURL(%s) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestZimpleScriptURL(t *testing.T) {
	const want = "https://vpos.infonet.com.py/checkout/javascript/dist/bancard-checkout-3.0.0.js"
	if got := ZimpleScriptURL(Production); got != want {
		t.Errorf("ZimpleScriptURL(Production) = %q, want %q", got, want)
	}
}

func TestCheckoutFormSnippet(t *testing.T) {
	const styles = `{"form-background-color": "#001b60", "button-text-color": "#fcfcfc"}`

	tests := []struct {
		name      string
		processID string
		container string
		styles    string
		want      string
	}{
		{
			name:      "with styles",
			processID: "i5fn*lx6niQel0QzWK1g",
			container: "iframe-container",
			styles:    styles,
			want:      `Bancard.Checkout.createForm('iframe-container', 'i5fn*lx6niQel0QzWK1g', ` + styles + `);`,
		},
		{
			name:      "without styles",
			processID: "WR-YY9JmxsEZV3hpVGA7",
			container: "iframe-container",
			want:      `Bancard.Checkout.createForm('iframe-container', 'WR-YY9JmxsEZV3hpVGA7');`,
		},
		{
			name:      "blank styles omitted",
			processID: "p1",
			container: "c",
			styles:    "   ",
			want:      `Bancard.Checkout.createForm('c', 'p1');`,
		},
		{
			name:      "invalid styles json omitted",
			processID: "p1",
			container: "c",
			styles:    `{"broken"`,
			want:      `Bancard.Checkout.createForm('c', 'p1');`,
		},
		{
			name:      "single quote in identifiers is escaped",
			processID: "a'b",
			container: "c'd",
			want:      `Bancard.Checkout.createForm('c\'d', 'a\'b');`,
		},
		{
			name:      "backslash in identifiers is escaped",
			processID: `a\b`,
			container: `c\d`,
			want:      `Bancard.Checkout.createForm('c\\d', 'a\\b');`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckoutFormSnippet(tt.processID, tt.container, tt.styles); got != tt.want {
				t.Errorf("CheckoutFormSnippet() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZimpleFormSnippet(t *testing.T) {
	const want = `Bancard.Zimple.createForm('iframe-container', 'p1');`
	if got := ZimpleFormSnippet("p1", "iframe-container", ""); got != want {
		t.Errorf("ZimpleFormSnippet() = %q, want %q", got, want)
	}
}
