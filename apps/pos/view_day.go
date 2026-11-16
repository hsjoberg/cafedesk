package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// dayView is the day so far, and closing it: the summary and the cash
// count on the left, the report as it prints on the right.
func (a *app) dayView(c *ui.Context) {
	r := a.report
	twoZones(c, func() {
		ui.Column(c).Padding(16, 12, 16, 20).Gap(14).Children(func() {
			a.screenHeader(c, "Day report", func() {
				if button(c, "print-report", "Print day report", btnPlain, r == nil).Clicked() {
					a.printReport()
				}
			})
			if r == nil {
				ui.Text(c, a.dayErr).TextColor(colChili)
				return
			}
			ui.Row(c).Grow(1).Gap(20).AlignItems(ui.Stretch).Children(func() {
				a.closePanel(c, *r)
			})
		})
	}, func() { a.reportStrip(c) })
}

func (a *app) closePanel(c *ui.Context, r DayReport) {
	ui.Column(c).Grow(1).Background(colTile).Radius(10).Padding(24).Gap(16).Children(func() {
		ui.Row(c).Gap(24).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "Net sales so far").FontSize(fsMeta).TextColor(colInkMuted)
				ui.Text(c, r.Net().AED()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
				ui.Textf(c, "%d orders since %s", r.Orders, r.Day.OpenedAt.Format("15:04")).TextColor(colInkMuted)

				ui.Text(c, "Count the cash").FontSize(fsItem).FontWeight(wMedium).Margin(24, 0, 0, 0)
				ui.Text(c, "Enter what's in the drawer, then close the day.").FontSize(fsMeta).TextColor(colInkMuted)
				counted := ui.Text(c, a.counted.value().String()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
				if a.counted.text == "" {
					counted.TextColor(colRule)
				}
				ui.Textf(c, "Expected: %s", r.ExpectedCash()).TextColor(colInkMuted).FontFeatures("tnum")
				if a.counted.text != "" {
					diff := a.counted.value() - r.ExpectedCash()
					switch {
					case diff < 0:
						ui.Text(c, "Short by "+(-diff).String()).FontWeight(wMedium).TextColor(colChili)
					case diff > 0:
						ui.Text(c, "Over by "+diff.String()).FontWeight(wMedium).TextColor(colChili)
					default:
						ui.Text(c, "The drawer matches.").FontWeight(wMedium)
					}
				}
				if a.dayErr != "" {
					ui.Text(c, a.dayErr).TextColor(colChili)
				}
				ui.Spacer(c)
				if button(c, "close-day", "Close the day", btnInk, false).Height(64).Clicked() {
					a.closeDay()
				}
			})
			amountPad(c, "count-", &a.counted, false)
		})
	})
}

// reportStrip shows the day report as it prints.
func (a *app) reportStrip(c *ui.Context) {
	r := a.report
	paper(c, func() {
		if r == nil {
			ui.Spacer(c)
			return
		}
		ui.Text(c, "Day report").FontWeight(wMedium)
		ui.Text(c, fmt.Sprintf("Opened %s by %s", r.Day.OpenedAt.Format("2 Jan 15:04"), r.Day.OpenedBy)).
			FontSize(fsMeta).TextColor(colInkMuted)
		dashedRule(c)
		touchScroll(c, "report", &a.reportDrag, func() {
			ui.Column(c).Gap(6).Children(func() {
				pairRow(c, fmt.Sprintf("Sales, %d orders", r.Orders), r.Gross.String(), false)
				pairRow(c, fmt.Sprintf("    Cash, %d", r.CashOrders), r.Cash.String(), false)
				pairRow(c, fmt.Sprintf("    Card, %d", r.CardOrders), r.Card.String(), false)
				pairRow(c, fmt.Sprintf("Refunds, %d", r.RefundCount), minus(r.Refunds()), false)
				pairRow(c, "Net sales", r.Net().AED(), true)
				pairRow(c, fmt.Sprintf("Voided, %d orders", r.VoidCount), r.VoidTotal.String(), false)
				dashedRule(c)
				pairRow(c, "Opening float", r.Day.OpeningFloat.String(), false)
				pairRow(c, "Cash sales", r.Cash.String(), false)
				pairRow(c, "Cash refunds", minus(r.RefundCash), false)
				pairRow(c, "Expected in drawer", r.ExpectedCash().String(), true)
				if len(r.Items) > 0 {
					dashedRule(c)
					ui.Text(c, "Items sold").FontWeight(wMedium)
					for _, it := range r.Items {
						pairRow(c, fmt.Sprintf("%d x %s", it.Qty, it.Name), it.Total.String(), false)
					}
				}
			})
		}).Grow(1)
	})
}
