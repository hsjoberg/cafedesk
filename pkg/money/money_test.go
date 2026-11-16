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
	if got := Fils(1600).AED(); got != "AED 16.00" {
		t.Errorf("AED() = %q", got)
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
