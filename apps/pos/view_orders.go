package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// screenHeader is the title row of a screen, with the way back.
func (a *app) screenHeader(c *ui.Context, title string, extra func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(fsKey).FontWeight(wBold)
		ui.Spacer(c)
		if extra != nil {
			extra()
		}
		if button(c, "back", "Back", btnPlain, false).Clicked() {
			a.screen, a.sheet = screenOrder, sheetNone
			a.refund, a.voiding = nil, nil
		}
	})
}

// ordersView lists the latest orders on the left, and shows the selected
// one on the right as its receipt, with what can be done to it.
func (a *app) ordersView(c *ui.Context) {
	twoZones(c, func() {
		ui.Column(c).Padding(16, 12, 16, 20).Gap(14).Children(func() {
			a.screenHeader(c, "Orders", nil)
			if a.ordersErr != "" {
				ui.Text(c, a.ordersErr).TextColor(colChili)
			}
			a.orderList(c)
			switch {
			case a.refund != nil:
				a.refundSheet(c)
			case a.voiding != nil:
				a.voidSheet(c)
			}
		})
	}, func() { a.orderReceipt(c) })
}

func (a *app) orderList(c *ui.Context) {
	if len(a.orders) == 0 && a.ordersErr == "" {
		ui.Text(c, "No orders yet.").TextColor(colInkMuted)
		return
	}
	touchScroll(c, "orders", &a.ordersDrag, func() {
		ui.Column(c).Gap(2).Children(func() {
			day := ""
			for _, o := range a.orders {
				if o.DayID != day {
					day = o.DayID
					label := dayLabel(o.DayDate)
					if a.day != nil && o.DayID == a.day.ID {
						label += ", open"
					}
					ui.Text(c, label).FontSize(fsMeta).TextColor(colInkMuted).Margin(12, 0, 4, 4)
				}
				a.orderRow(c, o)
			}
		})
	}).Grow(1).Background(colTile).Radius(4).Padding(4, 8)
}

func dayLabel(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("Monday 2 January")
}

func (a *app) orderRow(c *ui.Context, o OrderSummary) {
	selected := a.order != nil && a.order.ID == o.ID
	row := ui.Row(c.Key("order-" + o.ID))
	bg := ui.Transparent
	switch {
	case selected:
		bg = colPaper
	case row.Pressed() && !a.ordersDrag.moved:
		bg = colTilePressed
	}
	busy := a.refund != nil || a.voiding != nil
	row.Height(52).PaddingX(12).Gap(14).AlignItems(ui.Center).Radius(4).Background(bg).
		Role(ui.RoleButton).Label("Order " + o.Code).AccessSelected(selected).Disabled(busy)
	row.Children(func() {
		ui.Text(c, o.Code).FontWeight(wMedium).FontFeatures("tnum").Width(80)
		ui.Text(c, o.CreatedAt.Format("15:04")).FontFeatures("tnum").TextColor(colInkMuted).Width(48)
		ui.Text(c, o.StaffName).TextColor(colInkMuted).SingleLine().Grow(1)
		switch {
		case o.Status == statusVoided:
			ui.Text(c, "Voided").FontSize(fsMeta).FontWeight(wMedium).TextColor(colChili)
		case o.Refunded > 0:
			ui.Text(c, "Refunded "+o.Refunded.String()).FontSize(fsMeta).TextColor(colInkMuted)
		}
		method := "Cash"
		if o.Method == payCard {
			method = "Card"
		}
		ui.Text(c, method).FontSize(fsMeta).TextColor(colInkMuted).Width(40)
		total := ui.Text(c, o.Total.String()).FontFeatures("tnum").Width(72).TextAlign(ui.End)
		if o.Status == statusVoided {
			total.Strikethrough().TextColor(colInkMuted)
		}
	})
	if row.Clicked() && !a.ordersDrag.dragging(c.Now()) {
		a.selectOrder(o.ID)
	}
}

// orderReceipt is the selected order as its receipt, with Reprint, Refund
// and Void.
func (a *app) orderReceipt(c *ui.Context) {
	o := a.order
	paper(c, func() {
		if o == nil {
			ui.Text(c, "Choose an order.").TextColor(colInkMuted)
			ui.Spacer(c)
			return
		}
		ui.Row(c).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, "Order "+o.Code).FontWeight(wMedium).FontFeatures("tnum")
			ui.Spacer(c)
			ui.Text(c, o.CreatedAt.Format("2 Jan 15:04")).FontSize(fsMeta).TextColor(colInkMuted)
		})
		ui.Text(c, "Served by "+o.StaffName).FontSize(fsMeta).TextColor(colInkMuted)
		if o.Status == statusVoided {
			ui.Text(c, fmt.Sprintf("Voided by %s at %s: %s", o.VoidedBy, o.VoidedAt.Format("15:04"), o.VoidReason)).
				FontSize(fsMeta).TextColor(colChili).Margin(8, 0, 0, 0)
		}
		dashedRule(c)
		ui.Column(c).Grow(1).Gap(10).Children(func() {
			for _, l := range o.Lines {
				ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
					ui.Textf(c, "%d", l.Qty).Width(26).FontWeight(wMedium).FontFeatures("tnum")
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, l.Item.Name)
						if mods := l.ModsText(); mods != "" {
							ui.Text(c, mods).FontSize(fsMeta).TextColor(colInkMuted)
						}
						if n := o.RefundedQty(l.ID); n > 0 {
							ui.Textf(c, "%d refunded", n).FontSize(fsMeta).TextColor(colChili)
						}
					})
					ui.Text(c, l.Total().String()).FontFeatures("tnum")
				})
			}
		})
		dashedRule(c)
		ui.Column(c).Gap(4).Children(func() {
			pairRow(c, "Total", o.Total.AED(), true)
			if o.Method == payCash {
				pairRow(c, "Cash", o.Tendered.String(), false)
				pairRow(c, "Change", o.Change.String(), false)
			} else {
				pairRow(c, "Card", o.Total.String(), false)
			}
		})
		for _, r := range o.Refunds {
			ui.Text(c, fmt.Sprintf("Refunded %s at %s, approved by %s: %s", r.Total.AED(), r.CreatedAt.Format("2 Jan 15:04"),
				r.ApprovedBy, r.Reason)).FontSize(fsMeta).TextColor(colChili).Margin(6, 0, 0, 0)
		}
		busy := a.refund != nil || a.voiding != nil
		ui.Row(c).Gap(8).Margin(16, 0, 0, 0).Children(func() {
			if button(c, "order-reprint", "Reprint", btnPlain, busy || o.Status != statusCompleted).Grow(1).PaddingX(8).Clicked() {
				a.reprintOrder()
			}
			if button(c, "order-refund", "Refund", btnPlain, busy || !o.Refundable()).Grow(1).PaddingX(8).Clicked() {
				a.startRefund()
			}
			if button(c, "order-void", "Void", btnDanger, busy || !a.canVoid()).Grow(1).PaddingX(8).Clicked() {
				a.startVoid()
			}
		})
	})
}

// reasonChoice offers the reasons for a void or a refund.
func reasonChoice(c *ui.Context, prefix string, chosen *string) {
	ui.Text(c, "Reason").FontWeight(wMedium)
	ui.Row(c).Gap(8).Wrap().Children(func() {
		for _, r := range reasons {
			kind := btnOption
			if *chosen == r {
				kind = btnInk
			}
			if button(c, prefix+r, r, kind, false).PaddingX(14).Clicked() {
				*chosen = r
			}
		}
	})
}

// refundSheet chooses how many of each line to refund, and why.
func (a *app) refundSheet(c *ui.Context) {
	r, o := a.refund, a.order
	if o == nil {
		return
	}
	total := r.total(*o)
	sheetPanel(c, func() {
		ui.Text(c, "Refund from "+o.Code).FontSize(fsKey).FontWeight(wBold)
		ui.Column(c).Gap(8).Children(func() {
			for _, l := range o.Lines {
				left := l.Qty - o.RefundedQty(l.ID)
				ui.Row(c.Key("refund-" + l.ID)).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						ui.Text(c, l.Item.Name).FontWeight(wMedium)
						ui.Textf(c, "%d sold, %d left to refund, %s each", l.Qty, left, l.UnitPrice()).
							FontSize(fsMeta).TextColor(colInkMuted)
					})
					id := l.ID
					if button(c, "less", "−", btnKey, r.qty[id] == 0).Width(60).Clicked() {
						a.refundStep(id, -1)
					}
					ui.Textf(c, "%d", r.qty[id]).FontSize(fsKey).FontWeight(wBold).FontFeatures("tnum").
						Width(36).TextAlign(ui.Center)
					if button(c, "more", "+", btnKey, r.qty[id] >= left).Width(60).Clicked() {
						a.refundStep(id, 1)
					}
				})
			}
		})
		reasonChoice(c, "refund-reason-", &r.reason)
		ui.Spacer(c)
		how := "Give the cash back from the drawer."
		if o.Method == payCard {
			how = "Refund it to the card on the card machine."
		}
		ui.Text(c, how).TextColor(colInkMuted)
		if r.err != "" {
			ui.Text(c, r.err).TextColor(colChili)
		}
		ui.Row(c).Gap(12).Children(func() {
			if button(c, "refund-cancel", "Cancel", btnOption, r.saving).Width(160).Height(64).Clicked() {
				a.refund = nil
			}
			label := "Refund " + total.AED()
			if button(c, "refund-confirm", label, btnMoney, total == 0 || r.saving).Grow(1).Height(64).Clicked() {
				a.confirmRefund()
			}
		})
	})
}

// voidSheet confirms a void, with its reason.
func (a *app) voidSheet(c *ui.Context) {
	v, o := a.voiding, a.order
	if o == nil {
		return
	}
	sheetPanel(c, func() {
		ui.Text(c, "Void order "+o.Code).FontSize(fsKey).FontWeight(wBold)
		ui.Text(c, "The order stays in the records, marked as voided, and no longer counts in the day's sales. "+
			"If the customer paid, give the money back.").TextColor(colInkMuted)
		reasonChoice(c, "void-reason-", &v.reason)
		ui.Spacer(c)
		if v.err != "" {
			ui.Text(c, v.err).TextColor(colChili)
		}
		ui.Row(c).Gap(12).Children(func() {
			if button(c, "void-cancel", "Cancel", btnOption, false).Width(160).Height(64).Clicked() {
				a.voiding = nil
			}
			if button(c, "void-confirm", "Void "+o.Code, btnDangerFill, false).Grow(1).Height(64).Clicked() {
				a.confirmVoid()
			}
		})
	})
}
