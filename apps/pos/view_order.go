package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	receiptStripWidth = 384
	tilesPerPage      = 12
)

// tileLayout is the menu wall's grid: 4 × 3 on wide screens, 3 × 4 on
// narrow ones such as 1024 × 768, so names keep room and a page still
// holds 12 tiles.
func tileLayout(c *ui.Context) (cols, rows int) {
	if w, _ := c.Size(); w < 1200 {
		return 3, 4
	}
	return 4, 3
}

// twoZones lays a screen out as the order screen is: a wide zone on the
// left, the paper strip's zone on the right.
func twoZones(c *ui.Context, left, right func()) {
	ui.Grid(c).Fill().ColumnTracks(ui.Fr(1), ui.Fixed(receiptStripWidth)).RowTracks(ui.Fr(1)).
		Children(func() {
			left()
			ui.Column(c).Padding(16, 20, 26, 8).Children(right)
		})
}

func (a *app) orderView(c *ui.Context) {
	twoZones(c, func() { a.menuWall(c) }, func() { a.receiptStrip(c) })
}

// menuWall is the left zone: categories, a page of item tiles, and the
// pager. Sheets open over it.
func (a *app) menuWall(c *ui.Context) {
	ui.Column(c).Padding(16, 12, 16, 20).Gap(14).Children(func() {
		a.topBar(c)
		a.tileGrid(c)
		a.pager(c)
		switch a.sheet {
		case sheetPay:
			a.paySheet(c)
		case sheetMenu:
			a.tillMenu(c)
		case sheetOptions:
			a.optionsSheet(c)
		}
	})
}

func (a *app) topBar(c *ui.Context) {
	busy := a.sheet != sheetNone
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		for _, cat := range a.catalog.Categories {
			a.categoryTab(c, cat, busy)
		}
		ui.Spacer(c)
		now := c.Now()
		ui.Text(c, now.Format("15:04")).FontFeatures("tnum").TextColor(colInkMuted).MarginX(12)
		c.After(time.Until(now.Truncate(time.Minute).Add(time.Minute)))
		if button(c, "till-menu", a.staff.Name, btnPlain, busy).MaxWidth(180).Clicked() {
			a.sheet = sheetMenu
		}
	})
}

func (a *app) categoryTab(c *ui.Context, cat Category, busy bool) {
	selected := cat.ID == a.category
	tab := ui.Row(c.Key("tab-" + cat.ID))
	bg := ui.Transparent
	switch {
	case selected:
		bg = colTile
	case tab.Pressed():
		bg = colTilePressed
	}
	band := ui.Transparent
	if selected {
		band = categoryColor(cat)
	}
	tab.Height(56).PaddingX(16).Gap(10).AlignItems(ui.Center).Radius(4).Background(bg).
		BorderWidth(0, 0, 4, 0).BorderColor(band).
		Role(ui.RoleTab).Label(cat.Name).AccessSelected(selected).Disabled(busy)
	tab.Children(func() {
		ui.Box(c).Size(10, 10).Radius(2).Background(categoryColor(cat))
		ui.Text(c, cat.Name).FontWeight(wMedium).SingleLine()
	})
	if tab.Clicked() {
		a.category, a.page = cat.ID, 0
	}
}

// pageOf returns the items of the current page and the number of pages.
func (a *app) pageOf() ([]Item, int) {
	items := a.catalog.ItemsIn(a.category)
	pages := max(1, (len(items)+tilesPerPage-1)/tilesPerPage)
	a.page = min(max(a.page, 0), pages-1)
	start := a.page * tilesPerPage
	return items[start:min(start+tilesPerPage, len(items))], pages
}

func (a *app) tileGrid(c *ui.Context) {
	items, _ := a.pageOf()
	cat, _ := a.catalog.Category(a.category)
	band := categoryColor(cat)
	cols, rows := tileLayout(c)
	tracks := make([]ui.Track, rows)
	for i := range tracks {
		tracks[i] = ui.Fr(1)
	}
	ui.Grid(c).Columns(cols).RowTracks(tracks...).Gap(10).Grow(1).Children(func() {
		for _, it := range items {
			a.tile(c, it, band)
		}
	})
}

// tile is one item: a signage band on top, the English and Arabic names,
// and the price at the bottom.
func (a *app) tile(c *ui.Context, it Item, band ui.Color) {
	t := ui.Column(c.Key(it.ID))
	bg := colTile
	if t.Pressed() {
		bg = colTilePressed
		t.Translate(0, 2)
	}
	t.Background(bg).Radius(4).Clip().Role(ui.RoleButton).Label(it.Name).Disabled(a.sheet != sheetNone)
	t.Children(func() {
		ui.Box(c).Height(8).Background(band)
		ui.Column(c).Grow(1).Padding(10, 14, 12, 14).Gap(2).Children(func() {
			ui.Text(c, it.Name).FontSize(fsItem).FontWeight(wMedium).MaxLines(2)
			if it.NameAR != "" {
				ui.Text(c, it.NameAR).TextColor(colInkMuted).Lang("ar").SingleLine()
			}
			ui.Spacer(c)
			ui.Text(c, it.Price.String()).FontSize(fsPrice).FontWeight(wMedium).FontFeatures("tnum")
		})
	})
	if t.Clicked() {
		a.addItem(it)
	}
}

func (a *app) pager(c *ui.Context) {
	_, pages := a.pageOf()
	busy := a.sheet != sheetNone
	ui.Row(c).Height(56).Gap(12).AlignItems(ui.Center).Children(func() {
		if pages <= 1 {
			return
		}
		if button(c, "page-prev", "Previous", btnPlain, a.page == 0 || busy).Width(160).Clicked() {
			a.page--
		}
		ui.Spacer(c)
		ui.Textf(c, "Page %d of %d", a.page+1, pages).FontSize(fsMeta).TextColor(colInkMuted)
		ui.Spacer(c)
		if button(c, "page-next", "Next", btnPlain, a.page >= pages-1 || busy).Width(160).Clicked() {
			a.page++
		}
	})
}

// receiptStrip is the order drawn as the receipt it will print.
func (a *app) receiptStrip(c *ui.Context) {
	paper(c, func() {
		ui.Row(c).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, "Order "+a.currentCode()).FontWeight(wMedium).FontFeatures("tnum")
			ui.Spacer(c)
			ui.Text(c, a.cfg.ShopName).FontSize(fsMeta).TextColor(colInkMuted).SingleLine()
		})
		a.printNotesView(c)
		dashedRule(c)
		if a.cart.Empty() {
			a.emptyReceipt(c)
		} else {
			a.receiptLines(c)
		}
		dashedRule(c)
		a.receiptFooter(c)
	})
}

// printNotesView lists prints that failed, each with Retry.
func (a *app) printNotesView(c *ui.Context) {
	for _, n := range a.printNotes {
		ui.Column(c.Key(fmt.Sprintf("note-%d", n.id))).Margin(10, 0, 0, 0).Padding(10, 12).Gap(8).
			Background(colTile).Radius(4).BorderWidth(0, 0, 0, 4).BorderColor(colChili).Children(func() {
			ui.Text(c, n.label+" not printed: "+n.err).FontSize(fsMeta).TextColor(colChili)
			ui.Row(c).Gap(8).Children(func() {
				id := n.id
				if button(c, "retry", "Retry", btnOption, false).Height(44).MinHeight(44).Clicked() {
					a.retryNote(id)
				}
				if button(c, "dismiss", "Dismiss", btnOption, false).Height(44).MinHeight(44).Clicked() {
					a.dismissNote(id)
				}
			})
		})
	}
}

func (a *app) emptyReceipt(c *ui.Context) {
	ui.Column(c).Grow(1).Gap(6).Children(func() {
		if a.lastCode == "" {
			ui.Text(c, "Tap an item to start an order.").TextColor(colInkMuted)
			return
		}
		ui.Text(c, "Order "+a.lastCode+" saved.").FontWeight(wMedium)
		if a.lastChange > 0 {
			ui.Text(c, "Change to give").FontSize(fsMeta).TextColor(colInkMuted).Margin(14, 0, 0, 0)
			ui.Text(c, a.lastChange.AED()).FontSize(fsChange).FontWeight(wBold).FontFeatures("tnum")
		}
		if a.printStatus != "" {
			ui.Text(c, a.printStatus).FontSize(fsMeta).TextColor(colInkMuted).Margin(14, 0, 0, 0)
		}
		ui.Spacer(c)
		if a.lastOrder != nil {
			if button(c, "reprint", "Reprint receipt", btnPlain, false).Clicked() {
				a.printOrder(*a.lastOrder, true)
			}
		}
	})
}

func (a *app) receiptLines(c *ui.Context) {
	touchScroll(c, "receipt-lines", &a.linesDrag, func() {
		ui.Column(c).Gap(2).Children(func() {
			for i, l := range a.cart.Lines {
				a.receiptLine(c, i, l)
			}
		})
	}).Grow(1)
}

// tapped reports a click on e that isn't the end of a drag of the receipt.
func (a *app) tapped(c *ui.Context, e ui.Element) bool {
	return e.Clicked() && !a.linesDrag.dragging(c.Now())
}

func (a *app) receiptLine(c *ui.Context, i int, l Line) {
	selected := a.selected == i
	busy := a.sheet != sheetNone
	row := ui.Column(c.Key(fmt.Sprintf("line-%d-%s", i, l.key())))
	bg := ui.Transparent
	if selected || (row.Pressed() && !a.linesDrag.moved) {
		bg = colTile
	}
	row.Padding(10, 8).Radius(4).Background(bg).Role(ui.RoleButton).
		Label(fmt.Sprintf("%d %s", l.Qty, l.Item.Name)).Disabled(busy)
	row.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Textf(c, "%d", l.Qty).Width(26).FontWeight(wMedium).FontFeatures("tnum")
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, l.Item.Name)
				if mods := l.ModsText(); mods != "" {
					ui.Text(c, mods).FontSize(fsMeta).TextColor(colInkMuted)
				}
			})
			ui.Text(c, l.Total().String()).FontFeatures("tnum")
		})
		if !selected {
			return
		}
		ui.Row(c).Gap(6).Margin(10, 0, 0, 0).AlignItems(ui.Center).Children(func() {
			if a.tapped(c, button(c, "dec", "−", btnKey, busy).Width(52)) {
				if a.cart.Dec(i) {
					a.selected = -1
				}
			}
			if a.tapped(c, button(c, "inc", "+", btnKey, busy).Width(52)) {
				a.cart.Inc(i)
			}
			if len(l.Item.Groups) > 0 && a.tapped(c, button(c, "options", "Options", btnOption, busy).PaddingX(10)) {
				a.openOptions(i)
			}
			ui.Spacer(c)
			remove := ui.Box(c.Key("remove")).Height(56).PaddingX(4).Center().Role(ui.RoleButton).Label("Remove").Disabled(busy)
			remove.Children(func() { ui.Text(c, "Remove").FontWeight(wMedium).TextColor(colChili) })
			if a.tapped(c, remove) {
				a.cart.Remove(i)
				a.selected = -1
			}
		})
	})
	if a.tapped(c, row) {
		if selected {
			a.selected = -1
		} else {
			a.selected = i
		}
	}
}

func (a *app) receiptFooter(c *ui.Context) {
	total := a.cart.Total()
	ui.Row(c).AlignItems(ui.Center).Height(28).Children(func() {
		if a.cart.Empty() {
			return
		}
		n := a.cart.Count()
		label := fmt.Sprintf("%d items", n)
		if n == 1 {
			label = "1 item"
		}
		ui.Text(c, label).FontSize(fsMeta).TextColor(colInkMuted)
		ui.Spacer(c)
		if a.sheet == sheetNone && ui.Text(c.Key("clear"), "Clear order").FontSize(fsMeta).FontWeight(wMedium).
			TextColor(colChili).Role(ui.RoleButton).Padding(6, 0).Clicked() {
			a.clearOrder()
		}
	})
	ui.Row(c).AlignItems(ui.Baseline).Margin(4, 0, 16, 0).Children(func() {
		ui.Text(c, "Total").FontSize(fsItem)
		ui.Spacer(c)
		amount := ui.Text(c, total.AED()).FontSize(fsTotal).FontWeight(wBold).FontFeatures("tnum")
		if a.cart.Empty() {
			amount.TextColor(colRule)
		}
	})
	label := "Charge " + total.AED()
	switch {
	case a.sheet == sheetPay:
		label = "Taking payment"
	case a.cart.Empty():
		label = "Charge"
	}
	if button(c, "charge", label, btnMoney, a.cart.Empty() || a.sheet != sheetNone).Height(72).Clicked() {
		a.startPayment()
	}
}
