package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/ui"
)

// TestSnapshots renders the screens to PNG files for design review when
// CAFEDESK_SNAPSHOTS names a directory.
func TestSnapshots(t *testing.T) {
	dir := os.Getenv("CAFEDESK_SNAPSHOTS")
	if dir == "" {
		t.Skip("set CAFEDESK_SNAPSHOTS to a directory to render the screens")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, size := range []struct {
		name string
		w, h int
	}{{"1366", 1366, 768}, {"1024", 1024, 768}} {
		save := func(tt *ui.Tester, name string) {
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, size.name+"-"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
		}
		ctx := context.Background()
		a, _ := newTestApp(t)
		a.checks.printers = []string{"EPSON TM-T20III Receipt", "Kitchen TM-T82"}
		a.checks.printersLoaded = true
		a.cfg.ReceiptPrinter = "EPSON TM-T20III Receipt"
		tt := ui.NewTester(a.view, size.w, size.h)
		mustClick(t, tt, "1")
		mustClick(t, tt, "2")
		save(tt, "01-lock")
		mustClick(t, tt, "3")
		mustClick(t, tt, "4")
		save(tt, "02-own-pin")
		a.pinChange = nil
		a.staff.MustChangePIN = false
		a.store.SetPIN(ctx, a.staff.ID, "9999")
		a.float.text = "150"
		save(tt, "03-start-day")
		a.startDay()
		tt.Frame()

		mustClick(t, tt, "Karak chai")
		mustClick(t, tt, "Karak chai")
		mustClick(t, tt, "Saffron karak")
		mustClick(t, tt, "Meals")
		mustClick(t, tt, "Chicken biryani")
		save(tt, "04-order")
		mustClick(t, tt, "Options")
		mustClick(t, tt, "Mild")
		mustClick(t, tt, "Extra parotta  +1.00")
		save(tt, "05-options")
		mustClick(t, tt, "Done")
		mustClick(t, tt, "Charge AED 19.00")
		mustClick(t, tt, "20.00")
		save(tt, "06-pay")
		mustClick(t, tt, "Complete sale")
		a.printNotes = []printNote{{id: 1, role: roleKitchen, label: "Kitchen ticket T1-001", err: "the printer is offline"}}
		save(tt, "07-done")
		a.printNotes = nil

		for i := 0; i < 5; i++ {
			a.addItem(a.catalog.Items[i%3])
			a.startPayment()
			a.method = []string{payCash, payCard}[i%2]
			a.tendered.set(money.FromDirhams(10))
			a.completeSale()
		}
		tt.Frame()
		mustClick(t, tt, "Owner")
		save(tt, "08-till-menu")
		mustClick(t, tt, "Orders")
		save(tt, "09-orders")
		mustClick(t, tt, "Refund")
		mustClick(t, tt, "Customer changed their mind")
		save(tt, "10-refund")
		a.refund = nil
		a.startVoid()
		a.voiding.reason = "Test order"
		save(tt, "11-void")
		a.voiding = nil
		a.screen = screenOrder
		a.approval = &approval{what: "Void T1-006"}
		save(tt, "12-approval")
		a.approval = nil

		a.openDayReport()
		a.counted.text = "160"
		save(tt, "13-day")

		a.openSettings()
		a.store.AddStaff(ctx, "Ravi", roleCashier, "4321")
		a.loadSettings()
		save(tt, "14-settings")
		a.editText("Name of the new person", "Anil", func(string) {})
		save(tt, "15-keyboard")
		a.keyboard = nil

		a.screen = screenChecks
		save(tt, "16-checks")

		// Phase 2: pairing, and a paired till's settings and sync.
		a.screen = screenSettings
		a.cfg.ServerURL = "https://cafedesk.up.railway.app"
		a.pairing = &pairState{code: "5191"}
		save(tt, "17-pairing")
		a.pairing = nil
		a.token, a.cfg.ShopID, a.cfg.ShopLabel, a.cfg.TerminalCode = "token", "shop", "Downtown", "T1"
		a.sync = syncState{lastOK: time.Date(2026, 11, 16, 20, 6, 0, 0, time.Local), pending: 2}
		a.settingsDrag.state.Y = 0
		save(tt, "18-settings-paired")
		a.sync.err = "can't reach the server: dial tcp: no such host"
		a.screen = screenOrder
		save(tt, "19-offline")

		// A new till, connecting.
		a.demo, a.token, a.cfg.ShopID, a.synced, a.staff = false, "", "local", false, nil
		a.cfg.ServerURL = "https://cafedesk.up.railway.app"
		a.sync = syncState{}
		a.pairing = &pairState{code: "5191"}
		save(tt, "20-setup")
		a.pairing = nil
		a.token, a.cfg.ShopID, a.sync.busy = "token", "shop", true
		save(tt, "21-setup-fetching")
	}
}
