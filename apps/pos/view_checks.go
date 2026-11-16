package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// checksView tests what the POS needs from this PC. Printers are tested in
// Settings. It is deliberately plain.
func (a *app) checksView(c *ui.Context) {
	ui.Column(c).Fill().Padding(16, 20).Gap(14).Children(func() {
		a.screenHeader(c, "Checks for this PC", nil)
		if a.cfgErr != "" {
			ui.Text(c, a.cfgErr).TextColor(colChili)
		}
		cols := 3
		if w, _ := c.Size(); w < 1200 {
			cols = 2
		}
		touchScroll(c, "checks", &a.checksDrag, func() {
			ui.Grid(c).Columns(cols).Gap(12).AlignItems(ui.Start).Children(func() {
				a.touchPanel(c)
				a.scrollPanel(c)
				a.arabicPanel(c)
				a.databasePanel(c)
				a.serverPanel(c)
			})
		}, &a.scrollTestDrag).Grow(1)
	})
}

// panel is a group of settings or a check: a title, a line on what it is,
// and its contents.
func panel(c *ui.Context, title, about string, body func()) ui.Element {
	return ui.Column(c).Background(colTile).Radius(4).Padding(16).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(fsItem).FontWeight(wMedium)
		if about != "" {
			ui.Text(c, about).FontSize(fsMeta).TextColor(colInkMuted)
		}
		body()
	})
}

// confirm is a button the person checking presses when something works;
// it fills with ink once pressed, and a second press takes it back.
func confirm(c *ui.Context, key, label string, done *bool) {
	kind := btnOption
	if *done {
		kind = btnInk
	}
	if button(c, key, label, kind, false).Clicked() {
		*done = !*done
	}
}

func (a *app) touchPanel(c *ui.Context) {
	panel(c, "Touch", "Put two fingers on the box at once, then lift them.", func() {
		pad := ui.Box(c.Key("touch-pad")).Height(140).Radius(4).Background(colPaper).
			Border(1, colRule).Center()
		down := 0
		for _, t := range pad.Touches() {
			if t.Phase < ui.TouchEnded {
				down++
			}
		}
		a.checks.fingersNow = down
		a.checks.maxFingers = max(a.checks.maxFingers, down)
		pad.Children(func() {
			ui.Textf(c, "%d now", down).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
		})
		msg := fmt.Sprintf("Most fingers at once: %d.", a.checks.maxFingers)
		switch {
		case a.checks.maxFingers >= 2:
			msg += " Multi-touch works."
		case a.checks.maxFingers == 1:
			msg += " Try two fingers together."
		}
		ui.Text(c, msg).FontSize(fsMeta)
		pointer := "The system reports a mouse as the main pointer."
		if c.Preferences().Pointer == ui.PointerCoarse {
			pointer = "The system reports a touch screen as the main pointer."
		}
		ui.Text(c, pointer).FontSize(fsMeta).TextColor(colInkMuted)
	})
}

func (a *app) scrollPanel(c *ui.Context) {
	panel(c, "Scrolling", "Drag the list with one finger. The whole Checks screen scrolls the same way.", func() {
		touchScroll(c, "scroll-test", &a.scrollTestDrag, func() {
			ui.Column(c).Padding(8, 12).Gap(6).Children(func() {
				for i := 1; i <= 30; i++ {
					ui.Textf(c, "Row %d", i).FontFeatures("tnum")
				}
			})
		}).Height(140).Background(colPaper).Border(1, colRule).Radius(4)
		confirm(c, "scroll-one", "One finger scrolls", &a.checks.scrollOne)
	})
}

func (a *app) arabicPanel(c *ui.Context) {
	panel(c, "Arabic text", "The words should join up and read from right to left.", func() {
		ui.Column(c).Padding(12).Background(colPaper).Border(1, colRule).Radius(4).Gap(4).Children(func() {
			ui.Text(c, "شاي كرك بالزعفران").FontSize(fsKey).Lang("ar")
			ui.Text(c, "أهلاً وسهلاً، الطلب رقم ١٢").Lang("ar")
			ui.Text(c, "Karak and كرك in one line")
		})
		confirm(c, "arabic-ok", "Arabic looks right", &a.checks.arabicOK)
	})
}

func (a *app) databasePanel(c *ui.Context) {
	panel(c, "Database", "Writes a row to this PC's database and reads it back.", func() {
		path := "Not open."
		if a.store != nil {
			path = a.store.Path()
		} else if a.storeErr != nil {
			path = "Not open: " + a.storeErr.Error()
		}
		ui.Text(c, path).FontSize(fsMeta).TextColor(colInkMuted).Selectable()
		if button(c, "check-db", "Write and read a row", btnOption, a.checks.db.busy).Clicked() {
			a.checkDatabase()
		}
		resultText(c, a.checks.db)
	})
}

func (a *app) serverPanel(c *ui.Context) {
	panel(c, "Server", "Calls the server at the address set in config.json.", func() {
		ui.Text(c, a.cfg.ServerURL).FontSize(fsMeta).TextColor(colInkMuted).Selectable()
		if button(c, "check-server", "Call the server", btnOption, a.checks.srv.busy).Clicked() {
			a.checkServer()
		}
		resultText(c, a.checks.srv)
	})
}
