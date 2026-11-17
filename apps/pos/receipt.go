package main

import (
	"bytes"
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cafedesk/pkg/money"
)

// PrintLine is a line of a printed document.
type PrintLine struct {
	Text   string
	Big    bool // double width and height
	Bold   bool
	Center bool
}

// PrintDoc is something to print: a receipt, a kitchen ticket or a report.
// It renders to ESC/POS for receipt printers, or to plain lines that the
// Windows driver draws.
type PrintDoc struct {
	Title string // the print job's name in Windows
	Lines []PrintLine
	Kick  bool // open the cash drawer first (ESC/POS only)
	// Image, when set, is what prints instead of the lines: a receipt drawn
	// as a picture, so Arabic prints on any receipt printer. The lines stay
	// as its text, in English.
	Image *image.Gray
}

// ESC/POS commands, as Epson's printers and most receipt printers take.
var (
	escInit       = []byte{0x1b, 0x40}                   // ESC @
	escAlignLeft  = []byte{0x1b, 0x61, 0x00}             // ESC a 0
	escAlignCtr   = []byte{0x1b, 0x61, 0x01}             // ESC a 1
	escBoldOn     = []byte{0x1b, 0x45, 0x01}             // ESC E 1
	escBoldOff    = []byte{0x1b, 0x45, 0x00}             // ESC E 0
	escDoubleSize = []byte{0x1d, 0x21, 0x11}             // GS ! 0x11
	escNormalSize = []byte{0x1d, 0x21, 0x00}             // GS ! 0
	escKick       = []byte{0x1b, 0x70, 0x00, 0x19, 0xfa} // ESC p 0: pulse the drawer
	escFeedCut    = []byte{0x1b, 0x64, 0x04, 0x1d, 0x56, 0x42, 0x00}
)

// ESCPOS returns the document as ESC/POS bytes, ending with a feed and a
// paper cut.
func (d PrintDoc) ESCPOS() []byte {
	var b bytes.Buffer
	b.Write(escInit)
	if d.Kick {
		b.Write(escKick)
	}
	if d.Image != nil {
		b.Write(rasterESCPOS(d.Image))
		b.Write(escFeedCut)
		return b.Bytes()
	}
	for _, l := range d.Lines {
		if l.Center {
			b.Write(escAlignCtr)
		}
		if l.Bold {
			b.Write(escBoldOn)
		}
		if l.Big {
			b.Write(escDoubleSize)
		}
		b.WriteString(ascii(l.Text) + "\n")
		if l.Big {
			b.Write(escNormalSize)
		}
		if l.Bold {
			b.Write(escBoldOff)
		}
		if l.Center {
			b.Write(escAlignLeft)
		}
	}
	b.Write(escFeedCut)
	return b.Bytes()
}

// Text returns the document's lines as text, centered lines padded to
// width, for tests and for drivers without ESC/POS.
func (d PrintDoc) Text(width int) []string {
	out := make([]string, 0, len(d.Lines))
	for _, l := range d.Lines {
		w := width
		if l.Big {
			w = width / 2
		}
		t := ascii(l.Text)
		if l.Center {
			t = center(t, w)
		}
		out = append(out, t)
	}
	return out
}

// docBuilder lays out a document for a printer width in characters.
type docBuilder struct {
	width int
	doc   PrintDoc
}

func newDoc(title string, width int) *docBuilder {
	return &docBuilder{width: width, doc: PrintDoc{Title: title}}
}

func (b *docBuilder) line(l PrintLine) { b.doc.Lines = append(b.doc.Lines, l) }
func (b *docBuilder) text(s string)    { b.line(PrintLine{Text: s}) }
func (b *docBuilder) blank()           { b.text("") }
func (b *docBuilder) rule()            { b.text(strings.Repeat("-", b.width)) }
func (b *docBuilder) heading(s string) {
	b.line(PrintLine{Text: s, Big: true, Bold: true, Center: true})
}

// header writes the business's details at the top of a customer's slip:
// its trade name, address and phone, or the till's shop name until the
// admin has sent them.
func (b *docBuilder) header(h ReceiptHeader, shopName string) {
	name := h.TradeName
	if name == "" {
		name = shopName
	}
	b.heading(name)
	for _, s := range []string{h.Address, phoneLine(h), licenceLine(h)} {
		if s != "" {
			b.line(PrintLine{Text: s, Center: true})
		}
	}
	b.blank()
}

func phoneLine(h ReceiptHeader) string {
	if h.Phone == "" {
		return ""
	}
	return "Tel " + h.Phone
}

func licenceLine(h ReceiptHeader) string {
	if h.LicenceNumber == "" {
		return ""
	}
	return "Trade licence " + h.LicenceNumber
}

// pair puts left and right on one line, with the right edge flush.
func (b *docBuilder) pair(left, right string, bold bool) {
	b.line(PrintLine{Text: spread(ascii(left), ascii(right), b.width), Bold: bold})
}

// item writes "2 x Name" with the amount at the right, wrapping long names,
// then the modifiers worth printing, indented.
func (b *docBuilder) item(qty int, name, amount string, mods []LineMod, big bool) {
	width := b.width
	if big {
		width /= 2
	}
	head := strconv.Itoa(qty) + " x " + ascii(name)
	room := width - len(amount) - 1
	if amount == "" {
		room = width
	}
	for len(head) > room {
		cut := strings.LastIndex(head[:room], " ")
		if cut <= 0 {
			cut = room
		}
		b.line(PrintLine{Text: head[:cut], Big: big, Bold: big})
		head = "    " + strings.TrimSpace(head[cut:])
	}
	if amount == "" {
		b.line(PrintLine{Text: head, Big: big, Bold: big})
	} else {
		b.line(PrintLine{Text: spread(head, amount, width), Big: big, Bold: big})
	}
	for _, m := range mods {
		if m.Default {
			continue
		}
		text := "    " + ascii(m.Name)
		if m.Delta != 0 && !big {
			text = spread(text, "+"+m.Delta.String(), width)
		}
		b.line(PrintLine{Text: text, Big: big})
	}
}

// Receipt is what a customer receipt prints. The receipt strip on screen
// shows the same lines.
type Receipt struct {
	ShopName  string
	Header    ReceiptHeader
	Footer    string
	Code      string
	Time      time.Time
	StaffName string
	Lines     []Line
	Total     money.Fils
	Method    string
	Tendered  money.Fils
	Change    money.Fils
	Copy      bool // a reprint
}

func receiptFor(o Order, cfg Config) Receipt {
	return Receipt{ShopName: cfg.ShopName, Header: cfg.Header, Footer: cfg.ReceiptFooter, Code: o.Code, Time: o.CreatedAt,
		StaffName: o.StaffName, Lines: o.Lines, Total: o.Total, Method: o.Method,
		Tendered: o.Tendered, Change: o.Change}
}

// Doc lays the receipt out for a printer of width characters.
func (r Receipt) Doc(width int) PrintDoc {
	b := newDoc("Receipt "+r.Code, width)
	b.header(r.Header, r.ShopName)
	if r.Copy {
		b.line(PrintLine{Text: "Copy", Bold: true, Center: true})
	}
	b.pair("Order "+r.Code, r.Time.Format("02 Jan 2006 15:04"), false)
	if r.StaffName != "" {
		b.text("Served by " + r.StaffName)
	}
	b.rule()
	for _, l := range r.Lines {
		b.item(l.Qty, l.Item.Name, l.Total().String(), l.Mods, false)
	}
	b.rule()
	b.pair("Total", r.Total.AED(), true)
	if r.Method == payCash {
		b.pair("Cash", r.Tendered.String(), false)
		b.pair("Change", r.Change.String(), false)
	} else {
		b.pair("Card", r.Total.String(), false)
	}
	b.blank()
	footer := r.Footer
	if footer == "" {
		footer = "Thank you"
	}
	b.line(PrintLine{Text: footer, Center: true})
	return b.doc
}

// kitchenDoc is the kitchen's ticket of an order: its kitchen items, large,
// without prices. ok is false if the order has none.
func kitchenDoc(o Order, width int) (PrintDoc, bool) {
	b := newDoc("Kitchen "+o.Code, width)
	b.heading(o.Code)
	b.line(PrintLine{Text: o.CreatedAt.Format("15:04") + "  " + o.StaffName, Center: true})
	b.rule()
	n := 0
	for _, l := range o.Lines {
		if l.Item.Kitchen {
			b.item(l.Qty, l.Item.Name, "", l.Mods, true)
			n++
		}
	}
	return b.doc, n > 0
}

// refundDoc is the slip printed for a refund, for the customer and the
// drawer.
func refundDoc(o Order, r Refund, cfg Config) PrintDoc {
	b := newDoc("Refund "+o.Code, cfg.ReceiptWidth)
	b.header(cfg.Header, cfg.ShopName)
	b.line(PrintLine{Text: "Refund", Bold: true, Center: true})
	b.pair("Order "+o.Code, r.CreatedAt.Format("02 Jan 2006 15:04"), false)
	b.text("Approved by " + r.ApprovedBy)
	b.text("Reason: " + r.Reason)
	b.rule()
	for _, l := range r.Lines {
		b.item(l.Qty, l.Name, "-"+l.Amount.String(), nil, false)
	}
	b.rule()
	method := "Cash back"
	if r.Method == payCard {
		method = "Back to the card"
	}
	b.pair(method, r.Total.AED(), true)
	return b.doc
}

// reportDoc prints a day report: the X report of an open day, or the Z
// report of a closed one.
func reportDoc(r DayReport, cfg Config) PrintDoc {
	title := "Day report"
	if !r.Day.Open() {
		title = "Day closed"
	}
	b := newDoc(title, cfg.ReceiptWidth)
	b.heading(cfg.ShopName)
	b.blank()
	b.line(PrintLine{Text: title, Bold: true, Center: true})
	b.pair("Opened", r.Day.OpenedAt.Format("02 Jan 15:04")+" "+r.Day.OpenedBy, false)
	switch {
	case r.Day.Auto():
		b.pair("Closed by itself", r.Day.ClosedAt.Format("02 Jan 15:04"), false)
	case !r.Day.Open():
		b.pair("Closed", r.Day.ClosedAt.Format("02 Jan 15:04")+" "+r.Day.ClosedBy, false)
	default:
		b.pair("Printed", r.GeneratedAt.Format("02 Jan 15:04"), false)
	}
	b.rule()
	b.pair(fmt.Sprintf("Sales, %d orders", r.Orders), r.Gross.String(), false)
	b.pair(fmt.Sprintf("  Cash, %d", r.CashOrders), r.Cash.String(), false)
	b.pair(fmt.Sprintf("  Card, %d", r.CardOrders), r.Card.String(), false)
	b.pair(fmt.Sprintf("Refunds, %d", r.RefundCount), minus(r.Refunds()), false)
	b.pair("Net sales", r.Net().AED(), true)
	b.pair(fmt.Sprintf("Voided, %d orders", r.VoidCount), r.VoidTotal.String(), false)
	b.rule()
	b.pair("At the start", r.Day.OpeningFloat.String(), false)
	b.pair("Cash sales", r.Cash.String(), false)
	b.pair("Cash refunds", minus(r.RefundCash), false)
	for _, m := range r.Movements {
		b.pair(movementLabel(m), signed(m.Amount), false)
	}
	b.pair("Expected in drawer", r.ExpectedCash().String(), true)
	if !r.Day.Open() && !r.Day.Counted {
		b.pair("Counted", "not counted", false)
	}
	if !r.Day.Open() && r.Day.Counted {
		b.pair("Counted", r.Day.CountedCash.String(), false)
		diff := r.Difference()
		label := "Difference"
		switch {
		case diff < 0:
			label = "Short"
		case diff > 0:
			label = "Over"
		}
		b.pair(label, diff.String(), true)
	}
	if len(r.Items) > 0 {
		b.rule()
		b.text("Items sold")
		for _, it := range r.Items {
			b.item(it.Qty, it.Name, it.Total.String(), nil, false)
		}
	}
	return b.doc
}

// testDoc is printed from Settings and the Checks screen.
func testDoc(cfg Config, role string) PrintDoc {
	if role == roleKitchen {
		o := Order{Code: "TEST", CreatedAt: time.Now(), StaffName: "Test", Lines: []Line{
			{Item: Item{Name: "Chicken biryani", Kitchen: true}, Qty: 2,
				Mods: []LineMod{{Name: "Mild"}, {Name: "Extra parotta", Delta: 100}}},
		}}
		d, _ := kitchenDoc(o, cfg.ReceiptWidth)
		return d
	}
	lines := []Line{
		{Item: Item{Name: "Karak chai", NameAR: "شاي كرك", Price: 100}, Qty: 2,
			Mods: []LineMod{{Name: "Less sugar", NameAR: "سكر أقل"}}},
		{Item: Item{Name: "Chicken biryani", NameAR: "برياني دجاج", Price: 1400}, Qty: 1},
	}
	r := Receipt{ShopName: cfg.ShopName, Header: cfg.Header, Footer: cfg.ReceiptFooter, Code: "TEST", Time: time.Now(),
		StaffName: "Test", Lines: lines, Method: payCash}
	for _, l := range lines {
		r.Total += l.Total()
	}
	r.Tendered = money.FromDirhams(20)
	r.Change = r.Tendered - r.Total
	d := r.Doc(cfg.ReceiptWidth)
	if !cfg.PlainReceipts {
		d.Image = r.Image(paperDots(cfg.ReceiptWidth))
	}
	return d
}

// minus formats an amount taken away: "-2.00", or "0.00" for none.
func minus(f money.Fils) string {
	if f == 0 {
		return f.String()
	}
	return "-" + f.String()
}

func spread(left, right string, width int) string {
	gap := width - len(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func center(s string, width int) string {
	if pad := (width - len(s)) / 2; pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// ascii replaces what a printer's font may lack with '?', so a line keeps
// its width. Arabic needs the receipt drawn as an image (a later phase).
func ascii(s string) string {
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r == '’' || r == '‘':
			b.WriteByte('\'')
		case r == '–' || r == '—' || r == '−':
			b.WriteByte('-')
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
