package main

import (
	"cafedesk/pkg/money"

	"github.com/egoist/mygo/ui"
)

// sheetPanel is a sheet over the menu wall.
func sheetPanel(c *ui.Context, children func()) {
	ui.Column(c).Absolute().Top(16).Left(20).Right(12).Bottom(16).Background(colTile).Radius(10).
		Shadow(0, 2, 12, 0, colScrim).Padding(24).Gap(20).Children(children)
}

// paySheet covers the menu wall while a payment is taken; the receipt
// stays in view beside it.
func (a *app) paySheet(c *ui.Context) {
	total := a.cart.Total()
	sheetPanel(c, func() {
		ui.Row(c).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, "Payment").FontSize(fsKey).FontWeight(wBold)
			ui.Spacer(c)
			ui.Text(c, total.AED()).FontSize(fsKey).FontWeight(wBold).FontFeatures("tnum")
		})
		ui.Row(c).Gap(8).Children(func() {
			for _, m := range []struct{ id, label string }{{payCash, "Cash"}, {payCard, "Card"}} {
				kind := btnOption
				if a.method == m.id {
					kind = btnInk
				}
				if button(c, "method-"+m.id, m.label, kind, a.saving).Width(140).Clicked() {
					a.method = m.id
				}
			}
		})
		ui.Row(c).Grow(1).Gap(24).AlignItems(ui.Stretch).Children(func() {
			if a.method == payCard {
				ui.Text(c, "Take "+total.AED()+" on the card machine, then complete the sale.").
					FontSize(fsItem).Grow(1)
				return
			}
			a.cashPanel(c, total)
			amountPad(c, "key-", &a.tendered, a.saving)
		})
		if a.payErr != "" {
			ui.Text(c, a.payErr).TextColor(colChili)
		}
		ui.Row(c).Gap(12).Children(func() {
			if button(c, "pay-cancel", "Cancel", btnOption, a.saving).Width(160).Height(64).Clicked() {
				a.cancelPayment()
			}
			if button(c, "pay-complete", "Complete sale", btnMoney, !a.canComplete()).Grow(1).Height(64).Clicked() {
				a.completeSale()
			}
		})
	})
}

func (a *app) cashPanel(c *ui.Context, total money.Fils) {
	tendered := a.tendered.value()
	ui.Column(c).Grow(1).Gap(6).Children(func() {
		ui.Text(c, "Cash received").FontSize(fsMeta).TextColor(colInkMuted)
		amount := ui.Text(c, tendered.String()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
		if a.tendered.text == "" {
			amount.TextColor(colInkMuted)
		}
		ui.Text(c, "Change").FontSize(fsMeta).TextColor(colInkMuted).Margin(12, 0, 0, 0)
		switch {
		case a.tendered.text == "":
			ui.Text(c, "Type the amount, or tap a note below.").TextColor(colInkMuted)
		case tendered < total:
			ui.Text(c, "Short by "+(total-tendered).String()).FontSize(fsItem).FontWeight(wMedium).TextColor(colChili)
		default:
			ui.Text(c, (tendered - total).String()).FontSize(fsChange).FontWeight(wBold).FontFeatures("tnum")
		}
		ui.Spacer(c)
		ui.Grid(c).Columns(2).Gap(8).Children(func() {
			for i, f := range quickAmounts(total) {
				label := f.String()
				if i == 0 {
					label = "Exact"
				}
				if button(c, "quick-"+f.String(), label, btnKey, a.saving).PaddingX(4).Height(60).Clicked() {
					a.tendered.set(f)
				}
			}
		})
	})
}
