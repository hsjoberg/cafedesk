package money

import "testing"

func TestString(t *testing.T) {
	cases := map[Fils]string{
		0:     "0.00",
		5:     "0.05",
		50:    "0.50",
		100:   "1.00",
		1650:  "16.50",
		-50:   "-0.50",
		12345: "123.45",
	}
	for f, want := range cases {
		if got := f.String(); got != want {
			t.Errorf("Fils(%d).String() = %q, want %q", f, got, want)
		}
	}
	if got := Fils(1600).WithCode(); got != "AED 16.00" {
		t.Errorf("WithCode() = %q", got)
	}
}

// Currencies of three decimals and of none.
func TestOtherCurrencies(t *testing.T) {
	defer Use(AED)
	kwd, ok := LookupCurrency("kwd")
	if !ok || kwd.Decimals != 3 || kwd.Unit() != 1000 {
		t.Fatalf("KWD %+v, %v", kwd, ok)
	}
	Use(kwd)
	if got := Fils(1650).WithCode(); got != "KWD 1.650" {
		t.Errorf("KWD: %q", got)
	}
	if got := Fils(-5).String(); got != "-0.005" {
		t.Errorf("KWD: %q", got)
	}
	if f, err := Parse("2.25"); err != nil || f != 2250 {
		t.Errorf("KWD Parse: %d, %v", f, err)
	}
	if _, err := Parse("1.2345"); err == nil {
		t.Error("KWD took four decimals")
	}
	if FromMajor(5) != 5000 {
		t.Errorf("KWD FromMajor: %d", FromMajor(5))
	}
	jpy, _ := LookupCurrency("JPY")
	Use(jpy)
	if got := Fils(1600).WithCode(); got != "JPY 1600" {
		t.Errorf("JPY: %q", got)
	}
	if f, err := Parse("1600"); err != nil || f != 1600 {
		t.Errorf("JPY Parse: %d, %v", f, err)
	}
	if _, err := Parse("16.5"); err == nil {
		t.Error("JPY took decimals")
	}
	if _, ok := LookupCurrency("XYZ"); ok {
		t.Error("found XYZ")
	}
}

func TestParse(t *testing.T) {
	ok := map[string]Fils{
		"":      0,
		"0":     0,
		"16":    1600,
		"16.":   1600,
		"16.5":  1650,
		"16.50": 1650,
		".5":    50,
		" 20 ":  2000,
	}
	for s, want := range ok {
		got, err := Parse(s)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"1.234", "abc", "1,5", "-3", "1.2.3", "99999999999"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", s)
		}
	}
}
