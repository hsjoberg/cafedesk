package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// settingsView is for managers: the shop, printers, staff and backups.
func (a *app) settingsView(c *ui.Context) {
	ui.Column(c).Fill().Padding(16, 20).Gap(14).Children(func() {
		a.screenHeader(c, "Settings", func() {
			if a.settings.by != nil {
				ui.Text(c, "Opened by "+a.settings.by.Name).FontSize(fsMeta).TextColor(colInkMuted).MarginX(8)
			}
		})
		resultText(c, a.settings.msg)
		columns := [][]func(*ui.Context){
			{a.syncPanel, a.shopPanel, a.staffPanel},
			{a.receiptPrinterPanel, a.backupPanel},
			{a.kitchenPanel, a.dayEndPanel, a.screenPanel},
		}
		if w, _ := c.Size(); w < 1200 {
			columns = [][]func(*ui.Context){
				{a.syncPanel, a.shopPanel, a.kitchenPanel, a.staffPanel},
				{a.receiptPrinterPanel, a.backupPanel, a.dayEndPanel, a.screenPanel},
			}
		}
		touchScroll(c, "settings", &a.settingsDrag, func() {
			ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
				for _, col := range columns {
					ui.Column(c).Grow(1).Basis(0).Gap(12).Children(func() {
						for _, p := range col {
							p(c)
						}
					})
				}
			})
		}).Grow(1)
	})
}

// settingRow is a setting's name and value, with a button to change it.
func settingRow(c *ui.Context, key, name, value string, muted bool, change func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, name).FontSize(fsMeta).TextColor(colInkMuted)
			v := ui.Text(c, value)
			if muted {
				v.TextColor(colInkMuted)
			}
		})
		if button(c, key, "Change", btnOption, false).Height(44).MinHeight(44).PaddingX(14).Clicked() {
			change()
		}
	})
}

// syncPanel is the till's connection to its admin.
func (a *app) syncPanel(c *ui.Context) {
	panel(c, "Admin connection", "The till gets the menu and staff from the admin and sends its sales there, whenever it is online.", func() {
		if a.token != "" && a.cfg.ShopID != "local" {
			ui.Text(c, "Connected as "+a.cfg.TerminalCode+" of "+a.cfg.ShopLabel+".").FontWeight(wMedium)
		}
		if msg, bad := a.syncSummary(); msg != "" {
			col := colInkMuted
			if bad {
				col = colChili
			}
			ui.Text(c, msg).FontSize(fsMeta).TextColor(col)
		}
		ui.Row(c).Gap(8).Children(func() {
			if button(c, "sync-now", "Update now", btnOption, !a.paired() || a.sync.busy).Grow(1).PaddingX(8).Clicked() {
				a.syncNow()
			}
			if button(c, "pair", "Connect again", btnOption, false).Grow(1).PaddingX(8).Clicked() {
				a.openPairing()
			}
		})
		settingRow(c, "server-url", "The admin's address", a.cfg.ServerURL, true, a.editServerURL)
	})
}

func (a *app) shopPanel(c *ui.Context) {
	about := "Printed at the top and bottom of receipts."
	if h := a.cfg.Header; h.TradeName != "" {
		about = "Receipts print the trade name and address from the admin's Shop page: " + h.TradeName + "."
	}
	panel(c, "Shop", about, func() {
		settingRow(c, "shop-name", "Shop name", a.cfg.ShopName, false, func() {
			a.editText("Shop name", a.cfg.ShopName, func(s string) {
				if s != "" {
					a.cfg.ShopName = s
					a.saveConfig()
				}
			})
		})
		footer, muted := a.cfg.ReceiptFooter, false
		if footer == "" {
			footer, muted = "Thank you (the default)", true
		}
		settingRow(c, "shop-footer", "Last line of receipts", footer, muted, func() {
			a.editText("Last line of receipts", a.cfg.ReceiptFooter, func(s string) {
				a.cfg.ReceiptFooter = s
				a.saveConfig()
			})
		})
	})
}

// printerChoice lists Windows's printers to choose one.
func (a *app) printerChoice(c *ui.Context, prefix, current string, choose func(string)) {
	if len(a.checks.printers) == 0 {
		ui.Text(c, "No printers found. Add the printer in Windows settings, then open Settings again.").
			FontSize(fsMeta).TextColor(colInkMuted)
		return
	}
	ui.Column(c).Gap(6).Children(func() {
		for _, name := range a.checks.printers {
			label := name
			if name == a.checks.defaultPrinter {
				label += " (Windows default)"
			}
			kind := btnOption
			if name == current {
				kind = btnInk
			}
			if button(c, prefix+name, label, kind, false).PaddingX(12).Clicked() {
				choose(name)
			}
		}
	})
}

// modeChoice chooses how a printer is driven.
func modeChoice(c *ui.Context, prefix string, mode *string, changed func()) {
	ui.Text(c, "How to print").FontSize(fsMeta).TextColor(colInkMuted)
	ui.Row(c).Gap(8).Children(func() {
		for _, m := range []struct{ id, label string }{{printESCPOS, "Receipt printer codes"}, {printWindows, "Windows driver"}} {
			kind := btnOption
			if *mode == m.id {
				kind = btnInk
			}
			if button(c, prefix+m.id, m.label, kind, false).Grow(1).PaddingX(8).Clicked() {
				*mode = m.id
				changed()
			}
		}
	})
}

// switchRow is a setting turned on and off.
func switchRow(c *ui.Context, key, label string, on *bool, changed func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Grow(1)
		if ui.Switch(c.Key(key), on).Label(label).Changed() {
			changed()
		}
	})
}

func (a *app) receiptPrinterPanel(c *ui.Context) {
	panel(c, "Receipt printer", "Receipts, refund slips and day reports print here.", func() {
		a.printerChoice(c, "receipt-printer-", a.cfg.ReceiptPrinter, func(name string) {
			a.cfg.ReceiptPrinter = name
			a.saveConfig()
		})
		modeChoice(c, "receipt-mode-", &a.cfg.PrintMode, a.saveConfig)
		ui.Text(c, "Paper").FontSize(fsMeta).TextColor(colInkMuted)
		ui.Row(c).Gap(8).Children(func() {
			for _, p := range []struct {
				label string
				width int
			}{{"80 mm", 48}, {"58 mm", 32}} {
				kind := btnOption
				if a.cfg.ReceiptWidth == p.width {
					kind = btnInk
				}
				if button(c, "paper-"+p.label, p.label, kind, false).Grow(1).Clicked() {
					a.cfg.ReceiptWidth = p.width
					a.saveConfig()
				}
			}
		})
		switchRow(c, "drawer", "Open the cash drawer on cash sales", &a.cfg.OpenDrawer, a.saveConfig)
		arabic := !a.cfg.PlainReceipts
		switchRow(c, "arabic", "Receipts in English and Arabic", &arabic, func() {
			a.cfg.PlainReceipts = !arabic
			a.saveConfig()
		})
		if a.cfg.PlainReceipts {
			ui.Text(c, "Receipts print in English only. The law asks for Arabic: turn this back on unless the printer "+
				"can't print it.").FontSize(fsMeta).TextColor(colChili)
		}
		if button(c, "test-receipt", "Print a test receipt", btnOption, a.settings.print.busy).Clicked() {
			a.testPrint(roleReceipt)
		}
		resultText(c, a.settings.print)
	})
}

func (a *app) kitchenPanel(c *ui.Context) {
	var cats []string
	for _, cat := range a.catalog.Categories {
		if cat.Kitchen {
			cats = append(cats, cat.Name)
		}
	}
	what := "Items marked for the kitchen print on a ticket there, without prices."
	if len(cats) > 0 {
		what = strings.Join(cats, " and ") + " print on a ticket for the kitchen, without prices."
	}
	panel(c, "Kitchen printer", what, func() {
		switchRow(c, "kitchen-on", "Print kitchen tickets", &a.cfg.KitchenEnabled, a.saveConfig)
		if !a.cfg.KitchenEnabled {
			return
		}
		a.printerChoice(c, "kitchen-printer-", a.cfg.KitchenPrinter, func(name string) {
			a.cfg.KitchenPrinter = name
			a.saveConfig()
		})
		modeChoice(c, "kitchen-mode-", &a.cfg.KitchenPrintMode, a.saveConfig)
		if button(c, "test-kitchen", "Print a test ticket", btnOption, a.settings.print.busy).Clicked() {
			a.testPrint(roleKitchen)
		}
	})
}

func (a *app) staffPanel(c *ui.Context) {
	if !a.demo {
		a.staffFromServer(c)
		return
	}
	panel(c, "Staff", "Everyone signs in with their own 4-digit PIN. Managers approve voids, refunds and closing the day.", func() {
		for _, st := range a.settings.staff {
			ui.Column(c.Key("staff-"+st.ID)).Gap(8).Padding(12).Background(colPaper).Border(1, colRule).Radius(4).
				Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
						name := ui.Text(c, st.Name).FontWeight(wMedium)
						role := "Cashier"
						if st.IsManager() {
							role = "Manager"
						}
						if !st.Active {
							role += ", turned off"
							name.TextColor(colInkMuted)
						}
						ui.Text(c, role).FontSize(fsMeta).TextColor(colInkMuted)
						if st.MustChangePIN {
							ui.Text(c, "still has PIN 1234").FontSize(fsMeta).TextColor(colChili)
						}
					})
					ui.Row(c).Gap(6).Wrap().Children(func() {
						small := func(key, label string) bool {
							return button(c, key, label, btnOption, false).Height(44).MinHeight(44).PaddingX(12).Clicked()
						}
						if small("rename", "Rename") {
							s := st
							a.editText("Name", st.Name, func(name string) {
								if name != "" {
									s.Name = name
									a.updateStaff(s)
								}
							})
						}
						if st.Active && small("pin", "Change PIN") {
							a.pinChange = &pinChange{staffID: st.ID, name: st.Name}
						}
						toggleRole := "Make manager"
						if st.IsManager() {
							toggleRole = "Make cashier"
						}
						if small("role", toggleRole) {
							s := st
							s.Role = roleManager
							if st.IsManager() {
								s.Role = roleCashier
							}
							a.updateStaff(s)
						}
						toggleActive := "Turn off"
						if !st.Active {
							toggleActive = "Turn on"
						}
						if small("active", toggleActive) {
							s := st
							s.Active = !st.Active
							a.updateStaff(s)
						}
					})
				})
		}
		if button(c, "add-staff", "Add a person", btnOption, false).Clicked() {
			a.addStaffName()
		}
	})
}

// staffFromServer lists the staff of a paired till, who are managed in
// the admin.
func (a *app) staffFromServer(c *ui.Context) {
	panel(c, "Staff", "Staff are managed in the admin, on the Staff page. Changes reach this till when it syncs.", func() {
		for _, st := range a.settings.staff {
			if !st.Active {
				continue
			}
			ui.Row(c.Key("staff-"+st.ID)).Gap(8).AlignItems(ui.Baseline).Padding(10, 12).Background(colPaper).
				Border(1, colRule).Radius(4).Children(func() {
				ui.Text(c, st.Name).FontWeight(wMedium)
				role := "Cashier"
				if st.IsManager() {
					role = "Manager"
				}
				ui.Text(c, role).FontSize(fsMeta).TextColor(colInkMuted)
				if st.MustChangePIN {
					ui.Text(c, "chooses a PIN at first sign-in").FontSize(fsMeta).TextColor(colInkMuted)
				}
			})
		}
	})
}

func (a *app) backupPanel(c *ui.Context) {
	panel(c, "Backups", fmt.Sprintf("A copy of the database every %d hours and when the day closes. The last %d are kept. A USB drive is safest.",
		int(backupEvery.Hours()), backupsKept), func() {
		ui.Text(c, "Folder").FontSize(fsMeta).TextColor(colInkMuted)
		ui.Text(c, a.cfg.backupDir()).Selectable()
		last := "Not backed up yet."
		if !a.settings.last.IsZero() {
			last = "Last backup " + a.settings.last.Format("2 Jan 15:04") + "."
		}
		ui.Text(c, last).FontSize(fsMeta).TextColor(colInkMuted)
		ui.Row(c).Gap(8).Children(func() {
			if button(c, "backup-folder", "Choose folder", btnOption, a.win == nil).Grow(1).PaddingX(8).Clicked() {
				a.chooseBackupFolder()
			}
			if button(c, "backup-now", "Back up now", btnOption, a.settings.backup.busy).Grow(1).PaddingX(8).Clicked() {
				a.backupNow()
				a.loadSettings()
			}
		})
		resultText(c, a.settings.backup)
	})
}

// dayEndTimes are the choices for when a day closes by itself.
var dayEndTimes = []string{"02:00", "03:00", "04:00", "05:00", "06:00", dayEndsOff}

// dayEndPanel chooses when a day still open closes by itself.
func (a *app) dayEndPanel(c *ui.Context) {
	panel(c, "End of the day", "A day still open closes by itself at this time, without a count, and the till locks. "+
		"Choose a time after the shop closes and before it opens. The next sale starts the next day.", func() {
		ui.Grid(c).Columns(3).Gap(8).Children(func() {
			for _, t := range dayEndTimes {
				label := t
				if t == dayEndsOff {
					label = "Never"
				}
				kind := btnOption
				if a.cfg.dayEnds() == t {
					kind = btnInk
				}
				if button(c, "day-ends-"+t, label, kind, false).PaddingX(8).Clicked() {
					a.cfg.DayEnds = t
					a.saveConfig()
				}
			}
		})
	})
}

func (a *app) screenPanel(c *ui.Context) {
	panel(c, "Start-up and screen", "", func() {
		login := a.openAtLogin
		switchRow(c, "login", "Start when Windows starts", &login, func() {
			a.checks.login = a.setStartAtLogin(login)
		})
		resultText(c, a.checks.login)
		full := a.cfg.FullScreen
		switchRow(c, "fullscreen", "Full screen", &full, func() { a.setFullScreen(full) })
	})
}
