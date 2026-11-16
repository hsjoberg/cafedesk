package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// tillMenu is the sheet behind the cashier's name: everything that isn't
// ringing up an order.
func (a *app) tillMenu(c *ui.Context) {
	sheetPanel(c, func() {
		ui.Row(c).AlignItems(ui.Baseline).Gap(12).Children(func() {
			ui.Text(c, a.staff.Name).FontSize(fsKey).FontWeight(wBold)
			if a.day != nil {
				ui.Text(c, fmt.Sprintf("Day started %s by %s", a.day.OpenedAt.Format("15:04"), a.day.OpenedBy)).
					FontSize(fsMeta).TextColor(colInkMuted)
			}
		})
		if msg, bad := a.syncSummary(); msg != "" {
			col := colInkMuted
			if bad {
				col = colChili
			}
			ui.Text(c, msg).FontSize(fsMeta).TextColor(col)
		}
		ui.Grid(c).Columns(2).Gap(10).Children(func() {
			if optionRow(c, "menu-orders", "Orders", "Reprint, refund or void an order.").Clicked() {
				a.openOrders()
			}
			if optionRow(c, "menu-day", "Day report", "Sales so far, and closing the day.").Clicked() {
				a.openDayReport()
			}
			if optionRow(c, "menu-settings", "Settings", "Printers, staff and backups. Managers only.").Clicked() {
				a.sheet = sheetNone
				a.openSettings()
			}
			if optionRow(c, "menu-checks", "Checks", "Test the touch screen, database and server.").Clicked() {
				a.screen, a.sheet = screenChecks, sheetNone
			}
			if optionRow(c, "menu-pin", "Change my PIN", "Choose new digits to sign in with.").Clicked() {
				a.sheet = sheetNone
				a.pinChange = &pinChange{staffID: a.staff.ID, name: a.staff.Name}
			}
			if optionRow(c, "menu-lock", "Lock the till", "Sign out. The next person enters their PIN.").Clicked() {
				a.lockTill()
			}
		})
		ui.Spacer(c)
		if button(c, "menu-close", "Back to the order", btnOption, false).Height(64).Clicked() {
			a.sheet = sheetNone
		}
	})
}

// optionsSheet changes the modifiers of a receipt line.
func (a *app) optionsSheet(c *ui.Context) {
	i := a.optionsLine
	if i < 0 || i >= len(a.cart.Lines) {
		a.sheet = sheetNone
		return
	}
	l := a.cart.Lines[i]
	chosen := map[string]bool{}
	for _, m := range l.Mods {
		chosen[m.ID] = true
	}
	sheetPanel(c, func() {
		ui.Row(c).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, l.Item.Name).FontSize(fsKey).FontWeight(wBold).Grow(1)
			ui.Text(c, l.UnitPrice().String()).FontSize(fsKey).FontWeight(wBold).FontFeatures("tnum")
		})
		for _, g := range a.catalog.GroupsOf(l.Item) {
			ui.Column(c).Gap(8).Children(func() {
				hint := "Choose one."
				if g.Multiple {
					hint = "Choose any."
				}
				ui.Row(c).Gap(10).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, g.Name).FontWeight(wMedium)
					ui.Text(c, hint).FontSize(fsMeta).TextColor(colInkMuted)
				})
				ui.Row(c).Gap(8).Wrap().Children(func() {
					for _, m := range g.Modifiers {
						label := m.Name
						if m.Delta != 0 {
							label += "  +" + m.Delta.String()
						}
						kind := btnOption
						if chosen[m.ID] {
							kind = btnInk
						}
						if button(c, "mod-"+m.ID, label, kind, false).Clicked() {
							a.chooseModifier(g, m)
						}
					}
				})
			})
		}
		ui.Spacer(c)
		if button(c, "options-done", "Done", btnInk, false).Height(64).Clicked() {
			a.sheet = sheetNone
		}
	})
}
