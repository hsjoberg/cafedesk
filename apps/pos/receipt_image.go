package main

import (
	"bytes"
	"fmt"
	"image"

	"github.com/egoist/mygo/ui"
)

// Receipts for customers print as a picture, drawn the way the screen
// draws text, so Arabic prints, joined and right to left, on any receipt
// printer: in ESC/POS mode as raster dots, in Windows driver mode as a
// bitmap. The UAE's consumer protection law asks for a receipt in Arabic;
// English stands beside it. Kitchen tickets and day reports, for staff,
// stay as text.

// Sizes on paper, in printer dots: 203 dots an inch, so 24 dots is about 3 mm.
const (
	dotsBody  = 24
	dotsSmall = 21
	dotsHead  = 32
	dotsTotal = 30
)

var (
	paperWhite = ui.Hex("#FFFFFF")
	paperBlack = ui.Hex("#000000")
)

// paperDots is a receipt printer's printable width in dots, at 203 dots
// an inch: 576 for 80 mm paper (48 characters), 384 for 58 mm (32).
func paperDots(chars int) int {
	if chars <= 32 {
		return 384
	}
	return 576
}

// Arabic for the receipt's labels.
const (
	arCopy     = "نسخة"
	arOrder    = "طلب"
	arDate     = "التاريخ"
	arServedBy = "الكاشير"
	arTotal    = "الإجمالي"
	arCash     = "نقداً"
	arChange   = "الباقي"
	arCard     = "بطاقة"
	arThanks   = "شكراً لكم"
	arRefund   = "استرداد"
	arReason   = "السبب"
	arApproved = "بموافقة"
	arCashBack = "نقداً"
	arToCard   = "إلى البطاقة"
	arPhone    = "هاتف"
	arLicence  = "رخصة تجارية"
)

// withArabic fills in the Arabic names of an order's items and options from
// the menu: an order read back from the database keeps only the English.
func (a *app) withArabic(o Order) Order {
	items, mods := map[string]string{}, map[string]string{}
	for _, it := range a.catalog.Items {
		items[it.ID] = it.NameAR
	}
	for _, g := range a.catalog.Groups {
		for _, m := range g.Modifiers {
			mods[m.ID] = m.NameAR
		}
	}
	lines := make([]Line, len(o.Lines))
	for i, l := range o.Lines {
		if l.Item.NameAR == "" {
			l.Item.NameAR = items[l.Item.ID]
		}
		ms := make([]LineMod, len(l.Mods))
		for j, m := range l.Mods {
			if m.NameAR == "" {
				m.NameAR = mods[m.ID]
			}
			ms[j] = m
		}
		l.Mods, lines[i] = ms, l
	}
	o.Lines = lines
	return o
}

// Image draws the receipt for a printer dots wide.
func (r Receipt) Image(dots int) *image.Gray {
	return drawPaper(dots, func(c *ui.Context) {
		paperHeader(c, r.Header, r.ShopName)
		if r.Copy {
			biCenter(c, "Copy", arCopy, dotsBody, true)
		}
		biPair(c, "Order", arOrder, r.Code, dotsBody, false)
		biPair(c, "Date", arDate, r.Time.Format("02 Jan 2006 15:04"), dotsBody, false)
		if r.StaffName != "" {
			biPair(c, "Served by", arServedBy, r.StaffName, dotsBody, false)
		}
		paperRule(c)
		for _, l := range r.Lines {
			paperItem(c, l.Qty, l.Item.Name, l.Item.NameAR, l.Total().String(), l.Mods)
		}
		paperRule(c)
		biPair(c, "Total", arTotal, r.Total.WithCode(), dotsTotal, true)
		if r.Method == payCash {
			biPair(c, "Cash", arCash, r.Tendered.String(), dotsBody, false)
			biPair(c, "Change", arChange, r.Change.String(), dotsBody, false)
		} else {
			biPair(c, "Card", arCard, r.Total.String(), dotsBody, false)
		}
		ui.Box(c).Height(16)
		if r.Footer != "" {
			paperCenter(c, r.Footer, dotsBody, false)
		} else {
			biCenter(c, "Thank you", arThanks, dotsBody, false)
		}
	})
}

// refundImage draws a refund slip for a printer dots wide.
func refundImage(o Order, r Refund, cfg Config, dots int) *image.Gray {
	arabic := map[string]string{} // by order line
	for _, l := range o.Lines {
		arabic[l.ID] = l.Item.NameAR
	}
	return drawPaper(dots, func(c *ui.Context) {
		paperHeader(c, cfg.Header, cfg.ShopName)
		biCenter(c, "Refund", arRefund, dotsHead, true)
		biPair(c, "Order", arOrder, o.Code, dotsBody, false)
		biPair(c, "Date", arDate, r.CreatedAt.Format("02 Jan 2006 15:04"), dotsBody, false)
		biPair(c, "Approved by", arApproved, r.ApprovedBy, dotsBody, false)
		biPair(c, "Reason", arReason, r.Reason, dotsBody, false)
		paperRule(c)
		for _, l := range r.Lines {
			paperItem(c, l.Qty, l.Name, arabic[l.LineID], "-"+l.Amount.String(), nil)
		}
		paperRule(c)
		if r.Method == payCard {
			biPair(c, "Back to the card", arToCard, r.Total.WithCode(), dotsTotal, true)
		} else {
			biPair(c, "Cash back", arCashBack, r.Total.WithCode(), dotsTotal, true)
		}
	})
}

// paperHeader is the business at the top of a customer's slip: the trade
// name and address in English and Arabic, the phone and the licence.
func paperHeader(c *ui.Context, h ReceiptHeader, shopName string) {
	if h.TradeName == "" && h.TradeNameAr == "" {
		paperCenter(c, shopName, dotsHead, true)
	} else {
		paperCenter(c, h.TradeName, dotsHead, true)
		paperCenter(c, h.TradeNameAr, dotsHead, true)
	}
	paperCenter(c, h.Address, dotsSmall, false)
	paperCenter(c, h.AddressAr, dotsSmall, false)
	if h.Phone != "" {
		biCenter(c, "Tel "+h.Phone, arPhone, dotsSmall, false)
	}
	if h.LicenceNumber != "" {
		biCenter(c, "Trade licence "+h.LicenceNumber, arLicence, dotsSmall, false)
	}
	ui.Box(c).Height(14)
}

func paperText(c *ui.Context, s string, size float32, bold bool) ui.Element {
	t := ui.Text(c, s).FontSize(size).TextColor(paperBlack)
	if bold {
		t.FontWeight(wBold)
	} else {
		t.FontWeight(wMedium) // regular strokes are thin on thermal paper
	}
	return t
}

// paperCenter is a centered line, left out when empty.
func paperCenter(c *ui.Context, s string, size float32, bold bool) {
	if s != "" {
		paperText(c, s, size, bold).TextAlign(ui.Center).FillWidth()
	}
}

// biCenter is English and Arabic side by side, centered.
func biCenter(c *ui.Context, en, ar string, size float32, bold bool) {
	ui.Row(c).FillWidth().Gap(12).Justify(ui.Center).AlignItems(ui.Baseline).Children(func() {
		paperText(c, en, size, bold)
		paperText(c, ar, size, bold)
	})
}

// biPair is a label in English and Arabic, and its value at the right.
func biPair(c *ui.Context, en, ar, value string, size float32, bold bool) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Baseline).Children(func() {
		paperText(c, en, size, bold)
		paperText(c, ar, size, bold)
		ui.Spacer(c)
		paperText(c, value, size, bold).FontFeatures("tnum").SingleLine().Shrink(0)
	})
}

// paperItem is "2 x Karak chai" and its amount, its Arabic name under it,
// and the options worth printing.
func paperItem(c *ui.Context, qty int, name, nameAR, amount string, mods []LineMod) {
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).MarginY(3).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			paperText(c, fmt.Sprintf("%d x %s", qty, name), dotsBody, false)
			if nameAR != "" {
				// In Arabic, Start is the right: the name ends by the amount.
				paperText(c, nameAR, dotsBody, false).TextAlign(ui.Start).FillWidth()
			}
			for _, m := range mods {
				if m.Default {
					continue
				}
				label := "+ " + m.Name
				if m.Delta != 0 {
					label += "  +" + m.Delta.String()
				}
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Baseline).Children(func() {
					paperText(c, label, dotsSmall, false).Grow(1)
					if m.NameAR != "" {
						paperText(c, m.NameAR, dotsSmall, false)
					}
				})
			}
		})
		paperText(c, amount, dotsBody, false).FontFeatures("tnum").SingleLine().Shrink(0)
	})
}

// paperRule is a dashed line across the paper.
func paperRule(c *ui.Context) {
	ui.Box(c).FillWidth().Height(2).BorderWidth(2, 0, 0, 0).BorderColor(paperBlack).
		BorderStyle(ui.BorderDashed).MarginY(10)
}

// drawPaper draws a slip dots wide, as tall as it needs, in black and white.
func drawPaper(dots int, body func(c *ui.Context)) *image.Gray {
	view := func(c *ui.Context) {
		t := posTheme()
		t.Background, t.Text, t.TextMuted = paperWhite, paperBlack, paperBlack
		c.SetTheme(t)
		ui.Column(c).Width(float32(dots)).Background(paperWhite).Padding(4, 6, 0, 6).Gap(4).Children(func() {
			body(c)
		})
	}
	for height := 1600; ; height *= 2 {
		img := ui.Render(view, dots, height, 1)
		gray, used := toDots(img)
		// Drawn to the bottom edge: it may go on, so draw it taller.
		if used < height-8 || height >= 16000 {
			return cropDots(gray, used+24)
		}
	}
}

// toDots makes a picture black and white, as a thermal printer prints it,
// and reports how many rows from the top hold anything.
func toDots(img *image.RGBA) (*image.Gray, int) {
	b := img.Bounds()
	gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	used := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			o := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			r, g, bl := int(img.Pix[o]), int(img.Pix[o+1]), int(img.Pix[o+2])
			if (r*299+g*587+bl*114)/1000 < 150 {
				gray.Pix[y*gray.Stride+x] = 0
				used = y + 1
			} else {
				gray.Pix[y*gray.Stride+x] = 255
			}
		}
	}
	return gray, used
}

func cropDots(g *image.Gray, height int) *image.Gray {
	if height >= g.Bounds().Dy() {
		return g
	}
	return g.SubImage(image.Rect(0, 0, g.Bounds().Dx(), height)).(*image.Gray)
}

// rasterESCPOS is a picture as ESC/POS raster commands (GS v 0), in bands
// of up to 255 rows, which every receipt printer's buffer takes.
func rasterESCPOS(img *image.Gray) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	perRow := (w + 7) / 8
	var out bytes.Buffer
	for y0 := 0; y0 < h; y0 += 255 {
		rows := min(255, h-y0)
		out.Write([]byte{0x1d, 0x76, 0x30, 0x00, byte(perRow), byte(perRow >> 8), byte(rows), byte(rows >> 8)})
		for y := y0; y < y0+rows; y++ {
			for xb := 0; xb < perRow; xb++ {
				var v byte
				for bit := 0; bit < 8; bit++ {
					if x := xb*8 + bit; x < w && img.GrayAt(b.Min.X+x, b.Min.Y+y).Y < 128 {
						v |= 0x80 >> bit
					}
				}
				out.WriteByte(v)
			}
		}
	}
	return out.Bytes()
}
