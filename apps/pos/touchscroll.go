package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// MyGo v0.9.2 doesn't scroll with a finger on Windows: a drag reaches the
// app as a mouse drag, which scrolls nothing (issue #1712; the fix, PR
// #1911, is not merged yet). On the shop's Windows 10 LTSC POS neither one
// nor two fingers scrolled. touchScroll does it from the raw touches,
// which MyGo does report, until MyGo handles it itself.

// dragThreshold is how far a finger moves before a touch is a drag rather
// than a tap, in DIPs.
const dragThreshold = 10

// tapGuard is how long after a drag ends a click still counts as part of
// it: Windows turns the finger lifting into a click on what is under it.
const tapGuard = 400 * time.Millisecond

// touchDrag is a scroll container's finger drag.
type touchDrag struct {
	state ui.ScrollState

	active  bool
	id      uint64
	startY  float32 // the scroll offset when the finger landed
	moved   bool    // the finger went past dragThreshold
	endedAt time.Time
}

// dragging reports whether a click now is the end of a drag, and should
// be ignored.
func (d *touchDrag) dragging(now time.Time) bool {
	return d.moved && (d.active || now.Sub(d.endedAt) < tapGuard)
}

// touchScroll builds a vertical scroll container that follows one finger.
// Its children are built by children. A drag that started in one of the
// inner containers doesn't scroll this one.
func touchScroll(c *ui.Context, key string, d *touchDrag, children func(), inner ...*touchDrag) ui.Element {
	s := ui.Scroll(c.Key(key)).TrackScroll(&d.state).Children(children)
	for _, in := range inner {
		if in.active {
			return s
		}
	}
	for _, t := range s.Touches() {
		switch t.Phase {
		case ui.TouchBegan:
			if !d.active {
				d.active, d.id, d.startY, d.moved = true, t.ID, d.state.Y, false
			}
		case ui.TouchMoved, ui.TouchStationary:
			if !d.active || t.ID != d.id {
				continue
			}
			dy := t.Y - t.StartY
			if dy > dragThreshold || dy < -dragThreshold {
				d.moved = true
			}
			if d.moved {
				d.state.Y = min(max(d.startY-dy, 0), d.state.MaxY)
			}
		case ui.TouchEnded, ui.TouchCanceled:
			if d.active && t.ID == d.id {
				d.active = false
				if d.moved {
					d.endedAt = c.Now()
					c.After(tapGuard) // redraw once the guard is over
				}
			}
		}
	}
	return s
}
