package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
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
	cfg := defaultConfig()
	cfg.PlainReceipts = true
	doc := testDoc(cfg, roleReceipt)
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

func TestReceiptHeader(t *testing.T) {
	cfg := defaultConfig()
	cfg.Header = ReceiptHeader{TradeName: "Karak Corner Cafeteria", TradeNameAr: "كافتيريا ركن الكرك",
		Address: "Shop 3, Downtown, Dubai", AddressAr: "محل 3، وسط المدينة، دبي", Phone: "04 123 4567"}
	text := strings.Join(receiptFor(testOrder(), cfg).Doc(48).Text(48), "\n")
	for _, want := range []string{"Karak Corner Cafeteria", "Shop 3, Downtown, Dubai", "Tel 04 123 4567"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, cfg.ShopName) || strings.Contains(text, "Trade licence") {
		t.Errorf("the till's shop name, or a licence not set:\n%s", text)
	}
}

// TestReceiptImage draws a receipt in English and Arabic, as it prints.
// CAFEDESK_SNAPSHOTS saves it as a PNG, to look at.
func TestReceiptImage(t *testing.T) {
	cfg := defaultConfig()
	cfg.Header = ReceiptHeader{TradeName: "Karak Corner Cafeteria", TradeNameAr: "كافتيريا ركن الكرك",
		Address: "Shop 3, Downtown, Dubai", AddressAr: "محل 3، وسط المدينة، دبي", Phone: "04 123 4567",
		LicenceNumber: "1234567"}
	o := testOrder()
	o.Lines[0].Item.NameAR = "شاي كرك"
	o.Lines[1].Mods[0].NameAR = "باروتا إضافية"
	for _, width := range []int{48, 32} {
		img := receiptFor(o, cfg).Image(paperDots(width))
		b := img.Bounds()
		if b.Dx() != paperDots(width) || b.Dy() < 400 || b.Dy() > 1600 {
			t.Fatalf("width %d: %v", width, b)
		}
		black := 0
		for _, v := range img.Pix {
			if v == 0 {
				black++
			}
		}
		if black < 2000 {
			t.Errorf("width %d: %d black dots", width, black)
		}
		raster := rasterESCPOS(img)
		perRow := (b.Dx() + 7) / 8
		bands := (b.Dy() + 254) / 255
		if !bytes.HasPrefix(raster, []byte{0x1d, 0x76, 0x30, 0x00, byte(perRow), 0}) || len(raster) != perRow*b.Dy()+8*bands {
			t.Errorf("width %d: raster of %d bytes", width, len(raster))
		}
		if dir := os.Getenv("CAFEDESK_SNAPSHOTS"); dir != "" {
			os.MkdirAll(dir, 0o755)
			f, err := os.Create(filepath.Join(dir, "receipt-"+map[int]string{48: "80mm", 32: "58mm"}[width]+".png"))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, img)
			f.Close()
		}
	}
	// It is what prints, unless receipts are plain.
	doc := testDoc(cfg, roleReceipt)
	if doc.Image == nil || !bytes.Contains(doc.ESCPOS(), []byte{0x1d, 0x76, 0x30}) || bytes.Contains(doc.ESCPOS(), []byte("Chicken")) {
		t.Error("the test receipt isn't a picture")
	}
}
