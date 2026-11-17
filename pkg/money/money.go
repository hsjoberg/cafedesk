// Package money holds amounts as whole units of a currency's smallest
// part, never floats: fils for dirhams, cents for dollars, baisa for the
// Omani rial. The type is called Fils, as the code began with dirhams; it
// is the smallest unit of whatever currency is in use.
package money

import (
	"errors"
	"strconv"
	"strings"
)

// Fils is an amount in the currency's smallest unit: 1650 is 16.50 in a
// currency of two decimals, 1.650 in one of three.
type Fils int64

// Currency is an ISO 4217 currency and how many decimals its amounts have.
type Currency struct {
	Code     string `json:"code"`
	Decimals int    `json:"decimals"`
}

// Currencies are the ones a business can use, by code.
var Currencies = []Currency{
	{"AED", 2}, {"SAR", 2}, {"QAR", 2}, {"KWD", 3}, {"BHD", 3}, {"OMR", 3}, {"JOD", 3}, {"IQD", 3},
	{"EGP", 2}, {"MAD", 2}, {"TND", 3}, {"LYD", 3}, {"TRY", 2},
	{"USD", 2}, {"CAD", 2}, {"MXN", 2}, {"BRL", 2},
	{"EUR", 2}, {"GBP", 2}, {"CHF", 2}, {"SEK", 2}, {"NOK", 2}, {"DKK", 2}, {"PLN", 2}, {"ISK", 0},
	{"INR", 2}, {"PKR", 2}, {"LKR", 2}, {"BDT", 2}, {"NPR", 2},
	{"PHP", 2}, {"IDR", 2}, {"MYR", 2}, {"SGD", 2}, {"THB", 2}, {"VND", 0}, {"CNY", 2}, {"HKD", 2},
	{"JPY", 0}, {"KRW", 0}, {"AUD", 2}, {"NZD", 2},
	{"ZAR", 2}, {"KES", 2}, {"NGN", 2}, {"GHS", 2}, {"ETB", 2},
}

// LookupCurrency finds a currency by its code, such as "AED" or "aed".
func LookupCurrency(code string) (Currency, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, c := range Currencies {
		if c.Code == code {
			return c, true
		}
	}
	return Currency{}, false
}

// AED is the UAE dirham, the currency until a business sets its own.
var AED = Currency{"AED", 2}

// current is the currency amounts are formatted and read in. An app uses
// one at a time: the till its business's.
var current = AED

// Use sets the currency amounts are formatted and read in.
func Use(c Currency) { current = c }

// Current is the currency in use.
func Current() Currency { return current }

// Unit is how many of the smallest unit make one: 100 for two decimals.
func (c Currency) Unit() int64 {
	u := int64(1)
	for range c.Decimals {
		u *= 10
	}
	return u
}

// FromMajor returns whole units of the currency, such as dirhams, as fils.
func FromMajor(n int64) Fils { return Fils(n * current.Unit()) }

// String formats the amount with the currency's decimals, as "16.00",
// "-0.50", "1.650" or "1600".
func (f Fils) String() string {
	sign := ""
	v := int64(f)
	if v < 0 {
		sign = "-"
		v = -v
	}
	u := current.Unit()
	whole := strconv.FormatInt(v/u, 10)
	if current.Decimals == 0 {
		return sign + whole
	}
	frac := strconv.FormatInt(v%u, 10)
	for len(frac) < current.Decimals {
		frac = "0" + frac
	}
	return sign + whole + "." + frac
}

// WithCode formats the amount with its currency's code, as "AED 16.00".
func (f Fils) WithCode() string { return current.Code + " " + f.String() }

// Mul returns the amount times n, as a line of n items.
func (f Fils) Mul(n int) Fils { return f * Fils(n) }

// ErrInvalid is returned for text that is not an amount.
var ErrInvalid = errors.New("money: not an amount")

// Parse reads an amount typed in whole units, with at most the currency's
// decimals: in dirhams "16", "16.5" and "16.50" are all 1650 fils. An
// empty string is zero.
func Parse(s string) (Fils, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if !digits(whole) || (hasDot && !digits(frac)) || len(frac) > current.Decimals {
		return 0, ErrInvalid
	}
	for len(frac) < current.Decimals {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > 1_000_000_000 {
		return 0, ErrInvalid
	}
	var fr int64
	if frac != "" {
		fr, _ = strconv.ParseInt(frac, 10, 64)
	}
	return Fils(w*current.Unit() + fr), nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
