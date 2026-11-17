package main

import (
	"sort"
	"strings"

	"cafedesk/pkg/money"
)

// LineMod is a modifier chosen for a line, copied from the catalog so the
// order keeps it as sold.
type LineMod struct {
	GroupID   string
	GroupName string
	ID        string
	Name      string
	NameAR    string // for receipts; not kept with the order, so filled in from the menu to print
	Delta     money.Fils
	Default   bool // the group's default: not worth printing on a receipt
}

// Line is an item in the order, its modifiers, and how many of it.
type Line struct {
	ID   string // for the database; set when the order is saved
	Item Item
	Mods []LineMod
	Qty  int
}

// UnitPrice is the item's price with its modifiers.
func (l Line) UnitPrice() money.Fils {
	p := l.Item.Price
	for _, m := range l.Mods {
		p += m.Delta
	}
	return p
}

// Total is the line's amount.
func (l Line) Total() money.Fils { return l.UnitPrice().Mul(l.Qty) }

// ModsText lists the modifiers worth showing, as "Less sugar, Extra parotta":
// defaults are left out.
func (l Line) ModsText() string {
	var names []string
	for _, m := range l.Mods {
		if !m.Default {
			names = append(names, m.Name)
		}
	}
	return strings.Join(names, ", ")
}

// key is the same for lines that are the same item with the same modifiers.
func (l Line) key() string {
	ids := make([]string, 0, len(l.Mods))
	for _, m := range l.Mods {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return l.Item.ID + "|" + strings.Join(ids, ",")
}

// Cart is the order being rung up.
type Cart struct {
	Lines []Line
}

// Add adds one of the item with those modifiers, to the line that has them
// if there is one. It returns the line's index.
func (c *Cart) Add(it Item, mods []LineMod) int {
	l := Line{Item: it, Mods: mods, Qty: 1}
	k := l.key()
	for i := range c.Lines {
		if c.Lines[i].key() == k {
			c.Lines[i].Qty++
			return i
		}
	}
	c.Lines = append(c.Lines, l)
	return len(c.Lines) - 1
}

// SetMods replaces line i's modifiers.
func (c *Cart) SetMods(i int, mods []LineMod) {
	if i >= 0 && i < len(c.Lines) {
		c.Lines[i].Mods = mods
	}
}

// Inc adds one to line i.
func (c *Cart) Inc(i int) {
	if i >= 0 && i < len(c.Lines) {
		c.Lines[i].Qty++
	}
}

// Dec takes one from line i, removing the line at zero. It reports whether
// the line was removed.
func (c *Cart) Dec(i int) bool {
	if i < 0 || i >= len(c.Lines) {
		return false
	}
	c.Lines[i].Qty--
	if c.Lines[i].Qty <= 0 {
		c.Remove(i)
		return true
	}
	return false
}

// Remove removes line i.
func (c *Cart) Remove(i int) {
	if i >= 0 && i < len(c.Lines) {
		c.Lines = append(c.Lines[:i], c.Lines[i+1:]...)
	}
}

// Total is the order's amount.
func (c *Cart) Total() money.Fils {
	var t money.Fils
	for _, l := range c.Lines {
		t += l.Total()
	}
	return t
}

// Count is the number of items, counting quantities.
func (c *Cart) Count() int {
	n := 0
	for _, l := range c.Lines {
		n += l.Qty
	}
	return n
}

// Empty reports whether the order has no lines.
func (c *Cart) Empty() bool { return len(c.Lines) == 0 }

// Clear empties the order.
func (c *Cart) Clear() { c.Lines = nil }
