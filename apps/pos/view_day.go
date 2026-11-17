package main

import (
	"fmt"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/ui"
)

// dayView is the day so far, and closing it: the summary and the cash
// count on the left, the report as it prints on the right.
func (a *app) dayView(c *ui.Context) {
	r := a.report
	twoZones(c, func() {
		ui.Column(c).Padding(16, 12, 16, 20).Gap(14).Children(func() {
			a.screenHeader(c, "Day report", func() {
				if button(c, "cash-move", "Cash in or out", btnPlain, a.firstDay).Clicked() {
					a.openCashMove()
				}
				if button(c, "print-report", "Print day report", btnPlain, r == nil).Clicked() {
					a.printReport()
				}
			})
			switch {
			case r != nil:
				ui.Row(c).Grow(1).Gap(20).AlignItems(ui.Stretch).Children(func() {
					a.closePanel(c, *r)
				})
			case a.day == nil:
				a.noDayPanel(c)
			default:
				ui.Text(c, a.dayErr).TextColor(colChili)
			}
		})
	}, func() { a.reportStrip(c) })
}

// noDayPanel is the Day report with no day open: the next sale starts one.
func (a *app) noDayPanel(c *ui.Context) {
	ui.Column(c).Background(colTile).Radius(10).Padding(24).Gap(8).Children(func() {
		ui.Text(c, "No day is open").FontSize(fsItem).FontWeight(wMedium)
		ui.Text(c, "The next sale starts one, with what the last day left in the drawer:").TextColor(colInkMuted)
		ui.Text(c, a.carry.AED()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
		if a.dayErr != "" {
			ui.Text(c, a.dayErr).TextColor(colChili)
		}
	})
}

func (a *app) closePanel(c *ui.Context, r DayReport) {
	ui.Column(c).Grow(1).Background(colTile).Radius(10).Padding(24).Gap(16).Children(func() {
		ui.Row(c).Gap(24).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "Net sales so far").FontSize(fsMeta).TextColor(colInkMuted)
				ui.Text(c, r.Net().AED()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
				ui.Textf(c, "%d orders since %s", r.Orders, r.Day.OpenedAt.Format("15:04")).TextColor(colInkMuted)
				if end, ok := dayEndAfter(r.Day.OpenedAt, a.cfg.dayEnds()); ok {
					ui.Textf(c, "If it's still open at %s, the day closes by itself, without a count.", end.Format("15:04")).
						FontSize(fsMeta).TextColor(colInkMuted)
				}

				ui.Text(c, "Count the cash").FontSize(fsItem).FontWeight(wMedium).Margin(20, 0, 0, 0)
				ui.Text(c, "Optional. Enter what's in the drawer to check it against every day since the last count.").
					FontSize(fsMeta).TextColor(colInkMuted)
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
				label := "Close the day"
				if a.counted.text == "" {
					label = "Close without counting"
				}
				if button(c, "close-day", label, btnInk, false).Height(64).Clicked() {
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
				pairRow(c, "In the drawer at the start", r.Day.OpeningFloat.String(), false)
				pairRow(c, "Cash sales", r.Cash.String(), false)
				pairRow(c, "Cash refunds", minus(r.RefundCash), false)
				for _, m := range r.Movements {
					pairRow(c, movementLabel(m), signed(m.Amount), false)
				}
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

// cashMovePanel puts cash into the drawer or takes it out: which, how
// much and why.
func (a *app) cashMovePanel(c *ui.Context) {
	m := a.cashMove
	ui.Column(c).Width(760).Background(colTile).Radius(10).Padding(28).Gap(16).Children(func() {
		ui.Text(c, "Cash in or out").FontSize(fsKey).FontWeight(wBold)
		ui.Text(c, "For cash that goes in or out of the drawer other than by a sale or a refund, so the count still adds up.").
			TextColor(colInkMuted)
		ui.Row(c).Gap(24).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).Gap(10).Children(func() {
				ui.Row(c).Gap(8).Children(func() {
					for _, o := range []struct {
						key, label string
						out        bool
					}{{"move-out", "Take out", true}, {"move-in", "Put in", false}} {
						kind := btnOption
						if m.out == o.out {
							kind = btnInk
						}
						if button(c, o.key, o.label, kind, m.saving).Grow(1).Clicked() && m.out != o.out {
							m.out, m.reason, m.err = o.out, "", ""
						}
					}
				})
				ui.Text(c, "Why").FontSize(fsMeta).TextColor(colInkMuted).Margin(6, 0, 0, 0)
				reasons := cashInReasons
				if m.out {
					reasons = cashOutReasons
				}
				ui.Grid(c).Columns(2).Gap(8).Children(func() {
					for _, r := range reasons {
						kind := btnOption
						if m.reason == r {
							kind = btnInk
						}
						if button(c, "reason-"+r, r, kind, m.saving).PaddingX(8).Clicked() {
							m.reason, m.err = r, ""
						}
					}
				})
				ui.Text(c, "Amount").FontSize(fsMeta).TextColor(colInkMuted).Margin(6, 0, 0, 0)
				amount := ui.Text(c, m.amount.value().String()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
				if m.amount.text == "" {
					amount.TextColor(colRule)
				}
				if m.err != "" {
					ui.Text(c, m.err).TextColor(colChili)
				}
			})
			amountPad(c, "move-", &m.amount, m.saving)
		})
		ui.Row(c).Gap(10).Children(func() {
			if button(c, "move-cancel", "Cancel", btnOption, m.saving).Grow(1).Clicked() {
				a.cashMove = nil
			}
			label := "Put in " + m.amount.value().AED()
			if m.out {
				label = "Take out " + m.amount.value().AED()
			}
			if button(c, "move-save", label, btnInk, m.saving).Grow(2).Height(64).Clicked() {
				a.saveCashMove()
			}
		})
	})
}

// movementLabel names cash put in or taken out on a report.
func movementLabel(m CashMovement) string {
	if m.Amount < 0 {
		return "Taken out, " + m.Reason
	}
	return "Put in, " + m.Reason
}

// signed formats an amount with its sign: "+50.00", "-500.00".
func signed(f money.Fils) string {
	if f > 0 {
		return "+" + f.String()
	}
	return f.String()
}
