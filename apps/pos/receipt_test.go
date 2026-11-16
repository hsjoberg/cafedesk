package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func testOrder() Order {
	return Order{
		Code:      "T1-012",
		CreatedAt: time.Date(2026, 11, 16, 12, 41, 0, 0, time.Local),
		StaffName: "Ravi",
		Lines: []Line{
			{Item: Item{Name: "Karak chai", Price: 100}, Qty: 2,
				Mods: []LineMod{{Name: "Normal sugar", Default: true}}},
			{Item: Item{Name: "Beef fry and parotta with an extra long name", Price: 1200, Kitchen: true}, Qty: 1,
				Mods: []LineMod{{Name: "Extra parotta", Delta: 100}}},
		},
		Total: 1500, Method: payCash, Tendered: 2000, Change: 500,
	}
}

func TestReceiptDoc(t *testing.T) {
	cfg := defaultConfig()
	for _, width := range []int{32, 48} {
		doc := receiptFor(testOrder(), cfg).Doc(width)
		for _, l := range doc.Lines {
			w := width
			if l.Big {
				w /= 2
			}
			if len(l.Text) > w {
				t.Errorf("width %d: line %q is %d long", width, l.Text, len(l.Text))
			}
		}
		text := strings.Join(doc.Text(width), "\n")
		for _, want := range []string{"Order T1-012", "Served by Ravi", "2 x Karak chai", "Extra parotta", "+1.00",
			"AED 15.00", "Change", "5.00", "Thank you"} {
			if !strings.Contains(text, want) {
				t.Errorf("width %d: receipt lacks %q:\n%s", width, want, text)
			}
		}
		if strings.Contains(text, "Normal sugar") {
			t.Errorf("width %d: prints a default modifier", width)
		}
	}
}

func TestKitchenDoc(t *testing.T) {
	doc, ok := kitchenDoc(testOrder(), 48)
	if !ok {
		t.Fatal("no kitchen ticket")
	}
	text := strings.Join(doc.Text(48), "\n")
	if strings.Contains(text, "Karak") || strings.Contains(text, "12.00") || !strings.Contains(text, "Extra parotta") {
		t.Errorf("kitchen ticket:\n%s", text)
	}
	o := testOrder()
	o.Lines = o.Lines[:1]
	if _, ok := kitchenDoc(o, 48); ok {
		t.Error("a ticket for an order with nothing for the kitchen")
	}
}

func TestReceiptASCII(t *testing.T) {
	cfg := defaultConfig()
	cfg.ShopName = "كافيه Cafedesk"
	for _, l := range testDoc(cfg, roleReceipt).Text(48) {
		if !isASCII(l) {
			t.Errorf("line %q is not ASCII", l)
		}
	}
}

func TestESCPOS(t *testing.T) {
	doc := testDoc(defaultConfig(), roleReceipt)
	b := doc.ESCPOS()
	if !bytes.HasPrefix(b, escInit) || !bytes.HasSuffix(b, escFeedCut) {
		t.Error("not framed by init and cut")
	}
	if bytes.Contains(b, escKick) {
		t.Error("kicked the drawer unasked")
	}
	if !bytes.Contains(b, []byte("Chicken biryani")) {
		t.Error("lacks the lines")
	}
	doc.Kick = true
	if !bytes.HasPrefix(doc.ESCPOS(), append(append([]byte{}, escInit...), escKick...)) {
		t.Error("no drawer kick")
	}
}
