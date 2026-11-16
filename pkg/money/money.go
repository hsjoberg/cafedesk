// Package money holds amounts of UAE dirhams as whole fils, never floats.
package money

import (
	"errors"
	"strconv"
	"strings"
)

// Fils is an amount of UAE dirhams in fils: 1 AED is 100 fils.
type Fils int64

// FromDirhams returns whole dirhams as fils.
func FromDirhams(aed int64) Fils { return Fils(aed * 100) }

// String formats the amount with two decimals, as "16.00" or "-0.50".
func (f Fils) String() string {
	sign := ""
	v := int64(f)
	if v < 0 {
		sign = "-"
		v = -v
	}
	frac := strconv.FormatInt(v%100, 10)
	if len(frac) == 1 {
		frac = "0" + frac
	}
	return sign + strconv.FormatInt(v/100, 10) + "." + frac
}

// AED formats the amount with its currency, as "AED 16.00".
func (f Fils) AED() string { return "AED " + f.String() }

// Mul returns the amount times n, as a line of n items.
func (f Fils) Mul(n int) Fils { return f * Fils(n) }

// ErrInvalid is returned for text that is not an amount.
var ErrInvalid = errors.New("money: not an amount")

// Parse reads an amount typed as dirhams, with at most two decimals:
// "16", "16.5" and "16.50" are all 1650 fils. An empty string is zero.
func Parse(s string) (Fils, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if !digits(whole) || (hasDot && !digits(frac)) || len(frac) > 2 {
		return 0, ErrInvalid
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > 1_000_000_000 {
		return 0, ErrInvalid
	}
	fr, _ := strconv.ParseInt(frac, 10, 64)
	return Fils(w*100 + fr), nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
