package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// view builds the window: the screen of the moment, and the sheets over it.
func (a *app) view(c *ui.Context) {
	c.SetTheme(a.theme)
	ui.Box(c).Fill().Children(func() {
		switch {
		case a.needsSetup():
			a.setupView(c)
		case a.staff == nil:
			a.lockView(c)
		case a.pinChange != nil && a.pinChange.forced:
			a.pinChangeView(c)
		case a.screen == screenSettings:
			a.settingsView(c)
		case a.screen == screenChecks:
			a.checksView(c)
		case a.firstDay:
			a.startDayView(c)
		case a.screen == screenOrders:
			a.ordersView(c)
		case a.screen == screenDay:
			a.dayView(c)
		default:
			a.orderView(c)
		}
		if a.pinChange != nil && !a.pinChange.forced {
			overlay(c, func() { a.pinChangePanel(c) })
		}
		if a.cashMove != nil && a.approval == nil {
			overlay(c, func() { a.cashMovePanel(c) })
		}
		if a.approval != nil {
			overlay(c, func() { a.approvalPanel(c) })
		}
		if a.pairing != nil && !a.needsSetup() {
			overlay(c, func() { a.pairPanel(c) })
		}
		if a.keyboard != nil {
			a.keyboardView(c)
		}
	})
}

// lockView asks for a PIN: the till's door.
func (a *app) lockView(c *ui.Context) {
	now := c.Now()
	ui.Row(c).Fill().Padding(32).Gap(72).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		ui.Column(c).Width(380).Gap(10).Children(func() {
			ui.Text(c, a.cfg.ShopName).FontSize(fsTotal).FontWeight(wBold)
			status := "No day is open. The next sale starts one."
			if a.firstDay {
				status = "No day yet. Sign in to start the till's first."
			}
			if a.day != nil {
				status = fmt.Sprintf("Day open since %s, started by %s.", a.day.OpenedAt.Format("15:04"), a.day.OpenedBy)
			}
			ui.Text(c, status).TextColor(colInkMuted)
			ui.Text(c, now.Format("15:04")).FontSize(fsKey).FontFeatures("tnum").TextColor(colInkMuted).Margin(16, 0, 0, 0)
			c.After(time.Until(now.Truncate(time.Minute).Add(time.Minute)))
			if a.storeErr != nil {
				ui.Text(c, "The database didn't open: "+a.storeErr.Error()).TextColor(colChili).Margin(16, 0, 0, 0)
			}
		})
		ui.Column(c).Width(400).Background(colTile).Radius(10).Padding(28).Gap(16).AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, "Enter your PIN").FontSize(fsItem).FontWeight(wMedium)
				l := a.lock
				waiting := now.Before(l.until)
				pinPad(c, "lock-", len(l.pin), l.shakeAt, l.checks || waiting, func(k string) { a.lockKey(k, now) })
				msg := l.err
				if waiting {
					msg = fmt.Sprintf("Too many wrong PINs. Try again in %d seconds.", int(time.Until(l.until).Seconds())+1)
					c.After(time.Second)
				}
				ui.Text(c, msg).FontSize(fsMeta).TextColor(colChili).Height(20)
			})
	})
}

// startDayView opens the till's first day: count the cash into the drawer.
// After that, days start with their first sale, from what the drawer
// carries over.
func (a *app) startDayView(c *ui.Context) {
	ui.Column(c).Fill().Padding(16, 20).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Signed in as "+a.staff.Name).TextColor(colInkMuted).Grow(1).AlignSelf(ui.Center)
			if button(c, "start-checks", "Checks", btnPlain, false).Clicked() {
				a.screen = screenChecks
			}
			if button(c, "start-settings", "Settings", btnPlain, false).Clicked() {
				a.openSettings()
			}
			if button(c, "start-lock", "Lock the till", btnPlain, false).Clicked() {
				a.lockTill()
			}
		})
		ui.Box(c).Grow(1).Center().Children(func() {
			ui.Column(c).Width(620).Background(colTile).Radius(10).Padding(28).Gap(18).Children(func() {
				ui.Text(c, "The till's first day").FontSize(fsKey).FontWeight(wBold)
				ui.Row(c).Gap(24).AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Text(c, "Count the cash in the drawer and enter it. From then on the till keeps track of it: "+
							"each day starts with what the day before left.").TextColor(colInkMuted)
						ui.Text(c, "Cash in the drawer").FontSize(fsMeta).TextColor(colInkMuted).Margin(16, 0, 0, 0)
						amount := ui.Text(c, a.float.value().String()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
						if a.float.text == "" {
							amount.TextColor(colRule)
						}
						if a.dayErr != "" {
							ui.Text(c, a.dayErr).TextColor(colChili)
						}
						ui.Spacer(c)
						if button(c, "start-day", "Start the day", btnInk, a.store == nil).Height(64).Clicked() {
							a.startDay()
						}
					})
					amountPad(c, "float-", &a.float, false).Height(4*64 + 24)
				})
			})
		})
	})
}

// pinChangeView is the forced change of the default PIN, the whole window.
func (a *app) pinChangeView(c *ui.Context) {
	ui.Box(c).Fill().Center().Children(func() { a.pinChangePanel(c) })
}

func (a *app) pinChangePanel(c *ui.Context) {
	p := a.pinChange
	ui.Column(c).Width(420).Background(colTile).Radius(10).Padding(28).Gap(14).AlignItems(ui.Center).
		Children(func() {
			title := "Choose a PIN for " + p.name
			if p.forced {
				title = "Choose your own PIN"
			}
			ui.Text(c, title).FontSize(fsItem).FontWeight(wMedium).TextAlign(ui.Center)
			step := "Type 4 digits."
			if p.forced && p.first == "" {
				step = "1234 is the starting PIN. Type 4 new digits that only you know."
			}
			if p.first != "" {
				step = "Type the same 4 digits again."
			}
			ui.Text(c, step).FontSize(fsMeta).TextColor(colInkMuted).TextAlign(ui.Center)
			pinPad(c, "newpin-", len(p.pin), time.Time{}, false, a.pinChangeKey)
			ui.Text(c, p.err).FontSize(fsMeta).TextColor(colChili).Height(20)
			if !p.forced {
				if button(c, "newpin-cancel", "Cancel", btnOption, false).FillWidth().Clicked() {
					a.pinChange = nil
					a.settings.adding = nil
				}
			} else if button(c, "newpin-lock", "Lock the till", btnOption, false).FillWidth().Clicked() {
				a.lockTill()
			}
		})
}

// approvalPanel asks for a manager's PIN.
func (a *app) approvalPanel(c *ui.Context) {
	ap := a.approval
	ui.Column(c).Width(420).Background(colTile).Radius(10).Padding(28).Gap(14).AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, "Manager PIN needed").FontSize(fsItem).FontWeight(wMedium)
			ui.Text(c, ap.what).FontSize(fsBody).TextColor(colInkMuted).TextAlign(ui.Center)
			pinPad(c, "approve-", len(ap.pin), time.Time{}, ap.checks, a.approvalKey)
			ui.Text(c, ap.err).FontSize(fsMeta).TextColor(colChili).Height(20)
			if button(c, "approve-cancel", "Cancel", btnOption, false).FillWidth().Clicked() {
				a.approval = nil
			}
		})
}

// setupView connects a new till to its admin: nothing else works until it
// has, and has fetched the menu and staff.
func (a *app) setupView(c *ui.Context) {
	connected := a.token != "" && a.cfg.ShopID != "local"
	if !connected && a.pairing == nil {
		a.pairing = &pairState{}
	}
	ui.Row(c).Fill().Padding(32).Gap(72).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		ui.Column(c).Width(380).Gap(10).Children(func() {
			ui.Text(c, "Cafedesk").FontSize(fsTotal).FontWeight(wBold)
			ui.Text(c, "Connect this till to your admin.").FontSize(fsItem).FontWeight(wMedium)
			ui.Text(c, "It gets the menu and staff from the admin, and sends its sales there. "+
				"When the internet is down it keeps selling, and sends them later.").TextColor(colInkMuted)
		})
		ui.Column(c).Width(460).Background(colTile).Radius(10).Padding(28).Gap(16).Children(func() {
			if connected {
				ui.Text(c, "Connected as "+a.cfg.TerminalCode+" of "+a.cfg.ShopLabel+".").FontSize(fsItem).FontWeight(wMedium)
				if a.sync.busy || a.sync.err == "" {
					ui.Text(c, "Getting the menu and staff from the admin.").TextColor(colInkMuted)
					return
				}
				ui.Text(c, "Couldn't get the menu and staff: "+a.sync.err).TextColor(colChili)
				if button(c, "setup-retry", "Try again", btnInk, false).Clicked() {
					a.syncNow()
				}
				return
			}
			settingRow(c, "setup-address", "The admin's address", a.cfg.ServerURL, false, a.editServerURL)
			ui.Box(c).Height(1).Background(colRule)
			ui.Text(c, "Code from the admin").FontSize(fsMeta).TextColor(colInkMuted)
			ui.Text(c, "In the admin, open Tills, choose Add a till, and type the code it shows.").FontSize(fsMeta)
			p := a.pairing
			ui.Column(c).AlignItems(ui.Center).Gap(14).Children(func() {
				codePad(c, "pair-", pairCodeLength, len(p.code), time.Time{}, p.busy, a.pairKey)
				msg, col := p.err, colChili
				if p.busy {
					msg, col = "Connecting.", colInkMuted
				}
				if msg == "" {
					msg = a.settings.msg.text
				}
				ui.Text(c, msg).FontSize(fsMeta).TextColor(col).TextAlign(ui.Center).MinHeight(20)
			})
		})
	})
}

// editServerURL types the admin's address on the keyboard.
func (a *app) editServerURL() {
	a.keyboard = &keyboard{title: "The admin's address, such as https://cafedesk.up.railway.app", text: a.cfg.ServerURL, url: true,
		done: func(s string) {
			if s == "" {
				return
			}
			if err := checkServerURL(s); err != nil {
				a.settings.msg = errResult("Address not changed: ", err)
				return
			}
			a.cfg.ServerURL = s
			a.saveConfig()
			a.settings.msg = result{}
		}}
}

// pairPanel takes a new code from the admin, to connect again, as after the
// till was disconnected in the admin.
func (a *app) pairPanel(c *ui.Context) {
	p := a.pairing
	ui.Column(c).Width(460).Background(colTile).Radius(10).Padding(28).Gap(14).AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, "Connect with a new code").FontSize(fsItem).FontWeight(wMedium)
			ui.Text(c, "In the admin, open Tills, choose this till, and show a new code. Type its 8 digits.").
				FontSize(fsMeta).TextColor(colInkMuted).TextAlign(ui.Center)
			codePad(c, "pair-", pairCodeLength, len(p.code), time.Time{}, p.busy, a.pairKey)
			msg, col := p.err, colChili
			if p.busy {
				msg, col = "Connecting.", colInkMuted
			}
			ui.Text(c, msg).FontSize(fsMeta).TextColor(col).TextAlign(ui.Center).MinHeight(20)
			if button(c, "pair-cancel", "Cancel", btnOption, p.busy).FillWidth().Clicked() {
				a.pairing = nil
			}
		})
}

// keyboard is text being typed on the on-screen keyboard: the POS has no
// other.
type keyboard struct {
	title string
	text  string
	shift bool
	url   bool // a web address: no capitals, and the keys addresses need
	done  func(string)
}

var keyRows = []string{"1234567890", "qwertyuiop", "asdfghjkl'", "zxcvbnm,.-"}
var urlKeyRows = []string{"1234567890", "qwertyuiop", "asdfghjkl_", "zxcvbnm.:/-"}

func (a *app) keyboardView(c *ui.Context) {
	k := a.keyboard
	ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(colScrimDeep).Justify(ui.End).
		Children(func() {
			ui.Column(c).Background(colTile).Padding(20, 24, 24, 24).Gap(12).Children(func() {
				ui.Text(c, k.title).FontSize(fsMeta).TextColor(colInkMuted)
				ui.Row(c).Background(colPaper).Border(1, colRule).Radius(4).Padding(12, 16).Children(func() {
					ui.Text(c, k.text).FontSize(fsKey)
					ui.Box(c).Width(2).Height(28).Background(colInk).Margin(0, 0, 0, 2) // the caret
				})
				upper := k.shift || (!k.url && (k.text == "" || strings.HasSuffix(k.text, " ")))
				rows := keyRows
				if k.url {
					rows = urlKeyRows
				}
				for r, row := range rows {
					ui.Row(c).Gap(6).Justify(ui.Center).Children(func() {
						for _, ch := range row {
							s := string(ch)
							if upper && r > 0 {
								s = strings.ToUpper(s)
							}
							if button(c, fmt.Sprintf("kb-%d-%c", r, ch), s, btnKey, false).Width(64).Height(56).PaddingX(0).Clicked() {
								k.text += s
								k.shift = false
							}
						}
					})
				}
				ui.Row(c).Gap(6).Justify(ui.Center).Children(func() {
					shift := btnOption
					if k.shift {
						shift = btnInk
					}
					if button(c, "kb-shift", "Shift", shift, false).Width(120).Clicked() {
						k.shift = !k.shift
					}
					if button(c, "kb-space", "Space", btnOption, false).Width(300).Clicked() {
						k.text += " "
					}
					if button(c, "kb-back", "Delete", btnOption, false).Width(120).Clicked() && k.text != "" {
						_, size := lastRune(k.text)
						k.text = k.text[:len(k.text)-size]
					}
					if button(c, "kb-cancel", "Cancel", btnOption, false).Width(120).Clicked() {
						a.keyboard = nil
					}
					if button(c, "kb-done", "Done", btnInk, false).Width(140).Clicked() {
						a.keyboard = nil
						k.done(strings.TrimSpace(k.text))
					}
				})
			})
		})
}

func lastRune(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xC0 != 0x80 {
			return []rune(s[i:])[0], len(s) - i
		}
	}
	return 0, 0
}
