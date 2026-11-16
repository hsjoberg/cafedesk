package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

type buttonKind int

const (
	btnPlain      buttonKind = iota // tile on concrete
	btnMoney                        // saffron: money moves
	btnDanger                       // chili text: removes something
	btnDangerFill                   // chili fill: confirms a void
	btnKey                          // paper key on a tile panel, as the numpad's
	btnOption                       // paper button on a tile panel, in body type
	btnInk                          // ink fill: the chosen option, or a step that moves no money
)

// button draws a touch button. Check Clicked on what it returns; a
// disabled button reports no clicks.
func button(c *ui.Context, key any, label string, kind buttonKind, disabled bool) ui.Element {
	b := ui.Box(c.Key(key))
	pressed := b.Pressed() && !disabled
	bg, pressedBg, fg := colTile, colTilePressed, colInk
	size, weight, radius := float32(fsBody), wMedium, float32(4)
	switch kind {
	case btnMoney:
		bg, pressedBg = colSaffron, colSaffronPressed
		size, radius = fsPrice, 10
	case btnDanger:
		fg = colChili
	case btnDangerFill:
		bg, pressedBg, fg = colChili, colChiliPressed, colPaper
	case btnKey:
		bg = colPaper
		size = fsKey
	case btnOption:
		bg = colPaper
	case btnInk:
		bg, pressedBg, fg = colInk, colInkPressed, colPaper
	}
	if pressed {
		bg = pressedBg
		b.Translate(0, 2)
	}
	if kind == btnKey || kind == btnOption {
		b.Border(1, colRule)
	}
	b.MinHeight(56).PaddingX(20).Radius(radius).Background(bg).Center().
		Role(ui.RoleButton).Label(label).Disabled(disabled)
	b.Children(func() {
		ui.Text(c, label).FontSize(size).FontWeight(weight).TextColor(fg).
			FontFeatures("tnum").SingleLine()
	})
	return b
}

// optionRow is a large choice with a title and a line saying what it does,
// as in the till menu.
func optionRow(c *ui.Context, key, title, desc string) ui.Element {
	r := ui.Column(c.Key(key))
	bg := colPaper
	if r.Pressed() {
		bg = colTilePressed
		r.Translate(0, 2)
	}
	r.Background(bg).Border(1, colRule).Radius(4).Padding(14, 18).Gap(4).
		Role(ui.RoleButton).Label(title)
	r.Children(func() {
		ui.Text(c, title).FontSize(fsItem).FontWeight(wMedium)
		ui.Text(c, desc).FontSize(fsMeta).TextColor(colInkMuted)
	})
	return r
}

// dashedRule is the torn-off line of a receipt.
func dashedRule(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).BorderWidth(1, 0, 0, 0).BorderColor(colRule).
		BorderStyle(ui.BorderDashed).MarginY(12)
}

// paper is a sheet of thermal paper with a perforated bottom edge: the
// receipt strip, an order on the Orders screen, the day report.
func paper(c *ui.Context, children func()) ui.Element {
	return ui.Column(c).Grow(1).Background(colPaper).Shadow(0, 2, 12, 0, colScrim).
		Padding(20, 22, 26, 22).Children(func() {
		children()
		perforation(c)
	})
}

// perforation bites semicircles out of the paper's bottom edge.
func perforation(c *ui.Context) {
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(-7).Justify(ui.SpaceEvenly).PassThrough().
		Children(func() {
			for i := 0; i < 18; i++ {
				ui.Box(c).Size(14, 14).Radius(7).Background(colConcrete)
			}
		})
}

// pairRow is a label and an amount on one line, as a receipt prints them.
func pairRow(c *ui.Context, label, amount string, strong bool) {
	ui.Row(c).AlignItems(ui.Baseline).Gap(12).Children(func() {
		l := ui.Text(c, label).Grow(1)
		v := ui.Text(c, amount).FontFeatures("tnum")
		if strong {
			l.FontWeight(wMedium)
			v.FontWeight(wBold)
		}
	})
}

// resultText shows an action's outcome.
func resultText(c *ui.Context, r result) {
	if r.text == "" {
		return
	}
	col := colInkMuted
	if r.err {
		col = colChili
	}
	ui.Text(c, r.text).FontSize(fsMeta).TextColor(col)
}

// overlay dims the whole window and centers its children above it.
func overlay(c *ui.Context, children func()) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(colScrimDeep).Center().
		Children(children)
}

// amountPad is a numpad for an amount, laid out as a calculator's: 7 8 9
// on top.
func amountPad(c *ui.Context, prefix string, e *amountEntry, disabled bool) ui.Element {
	keys := []struct{ key, label string }{
		{"7", "7"}, {"8", "8"}, {"9", "9"},
		{"4", "4"}, {"5", "5"}, {"6", "6"},
		{"1", "1"}, {"2", "2"}, {"3", "3"},
		{".", "."}, {"0", "0"}, {"back", "Delete"},
	}
	return ui.Grid(c).Columns(3).RowTracks(ui.Fr(1), ui.Fr(1), ui.Fr(1), ui.Fr(1)).Gap(8).Width(3*92 + 16).
		MaxHeight(4*80 + 24).AlignSelf(ui.End).
		Children(func() {
			for _, k := range keys {
				b := button(c, prefix+k.key, k.label, btnKey, disabled)
				if k.key == "back" {
					b.PaddingX(4)
				}
				if b.Clicked() {
					e.press(k.key)
				}
			}
		})
}

// pinPad is four PIN dots and a numpad laid out as a phone's: 1 2 3 on
// top. A wrong PIN shakes the dots.
func pinPad(c *ui.Context, prefix string, typed int, shakeAt time.Time, disabled bool, onKey func(string)) {
	dots := ui.Row(c).Gap(18).Justify(ui.Center).Height(28).AlignItems(ui.Center)
	if since := c.Now().Sub(shakeAt); !shakeAt.IsZero() && since < 400*time.Millisecond {
		t := float64(since) / float64(400*time.Millisecond)
		dots.Translate(float32(math.Sin(t*math.Pi*6)*10*(1-t)), 0)
		c.Invalidate()
	}
	dots.Role(ui.RoleStatus).Label("PIN digits entered").Children(func() {
		for i := 0; i < pinLength; i++ {
			d := ui.Box(c).Size(18, 18).Radius(9).Border(2, colInk)
			if i < typed {
				d.Background(colInk)
			}
		}
	})
	keys := []struct{ key, label string }{
		{"1", "1"}, {"2", "2"}, {"3", "3"},
		{"4", "4"}, {"5", "5"}, {"6", "6"},
		{"7", "7"}, {"8", "8"}, {"9", "9"},
		{"clear", "Clear"}, {"0", "0"}, {"back", "Delete"},
	}
	ui.Grid(c).Columns(3).Gap(8).Width(3*96 + 16).Children(func() {
		for _, k := range keys {
			b := button(c, prefix+k.key, k.label, btnKey, disabled).Height(68)
			if k.key == "clear" || k.key == "back" {
				b.PaddingX(4)
			}
			if b.Clicked() {
				onKey(k.key)
			}
		}
	})
}
