package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// drag moves finger 1 from (x, y) by dy, in steps, as a finger does.
func drag(tt *ui.Tester, x, y, dy float32) {
	tt.TouchDown(1, x, y)
	for i := 1; i <= 5; i++ {
		tt.TouchMove(1, x, y+dy*float32(i)/5)
	}
	tt.TouchUp(1, x, y+dy)
}

func TestTouchScrollReceipt(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	for _, it := range a.catalog.ItemsIn("meals") {
		a.cart.Add(it, nil) // 14 lines: more than the receipt shows
	}
	tt := ui.NewTester(a.view, 1366, 768)
	a.linesDrag.state.Y = 0
	tt.Frame()
	if a.linesDrag.state.MaxY <= 0 {
		t.Fatalf("the receipt doesn't overflow (MaxY %v)", a.linesDrag.state.MaxY)
	}

	r, ok := tt.Find("Mutton biryani")
	if !ok {
		t.Fatal("receipt line not found")
	}
	drag(tt, r.X+r.W/2, r.Y+r.H/2, -200)
	if a.linesDrag.state.Y <= 0 {
		t.Errorf("a one-finger drag didn't scroll the receipt (Y %v)", a.linesDrag.state.Y)
	}
	if a.selected != -1 {
		t.Errorf("the drag selected line %d", a.selected)
	}

	// Windows turns the end of the drag into a click on what is under the
	// finger: it must not select a line.
	r, _ = tt.Find("Shawarma plate")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if a.selected != -1 {
		t.Errorf("the click ending a drag selected line %d", a.selected)
	}
	// A tap after the guard does.
	tt.Advance(time.Second)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if a.selected == -1 {
		t.Error("a tap after the drag didn't select the line")
	}
}

func TestTouchScrollNested(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	a.screen = screenChecks
	tt := ui.NewTester(a.view, 1024, 768)
	r, ok := tt.Find("Row 2")
	if !ok {
		t.Fatal("scroll test list not found")
	}
	drag(tt, r.X+r.W/2, r.Y+r.H/2, -100)
	if a.scrollTestDrag.state.Y <= 0 {
		t.Errorf("the test list didn't scroll (Y %v)", a.scrollTestDrag.state.Y)
	}
	if a.checksDrag.state.Y != 0 {
		t.Errorf("a drag in the test list also scrolled the screen (Y %v)", a.checksDrag.state.Y)
	}

	r, _ = tt.Find("Touch")
	tt.Advance(time.Second)
	drag(tt, r.X+r.W/2, r.Y+r.H/2, -150)
	if a.checksDrag.state.Y <= 0 {
		t.Errorf("a drag on the screen didn't scroll it (Y %v)", a.checksDrag.state.Y)
	}
}
