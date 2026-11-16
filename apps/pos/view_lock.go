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
		case a.staff == nil:
			a.lockView(c)
		case a.pinChange != nil && a.pinChange.forced:
			a.pinChangeView(c)
		case a.screen == screenSettings:
			a.settingsView(c)
		case a.screen == screenChecks:
			a.checksView(c)
		case a.day == nil:
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
		if a.approval != nil {
			overlay(c, func() { a.approvalPanel(c) })
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
			status := "No day is open. Sign in to start one."
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

// startDayView opens a business day: count the float into the drawer.
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
				ui.Text(c, "No day is open").FontSize(fsKey).FontWeight(wBold)
				ui.Row(c).Gap(24).AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Text(c, "Count the cash in the drawer and enter it. Closing the day compares the drawer with it.").
							TextColor(colInkMuted)
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

// keyboard is text being typed on the on-screen keyboard: the POS has no
// other.
type keyboard struct {
	title string
	text  string
	shift bool
	done  func(string)
}

var keyRows = []string{"1234567890", "qwertyuiop", "asdfghjkl'", "zxcvbnm,.-"}

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
				upper := k.shift || k.text == "" || strings.HasSuffix(k.text, " ")
				for r, row := range keyRows {
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
