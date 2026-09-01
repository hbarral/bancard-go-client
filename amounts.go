package bancard

import "strconv"

// formatAmount converts v to the canonical VPOS monetary string with two
// decimal digits and a dot separator, e.g. 10330 -> "10330.00". It uses
// fixed 'f' formatting to avoid scientific notation for large values.
func formatAmount(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
