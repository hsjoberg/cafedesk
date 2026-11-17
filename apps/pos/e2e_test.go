package main

import (
	"context"
	"os"
	"testing"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/ui"
)

// TestEndToEnd runs the till against a real server, when one is given:
//
//	CAFEDESK_E2E_SERVER  the server's address
//	CAFEDESK_E2E_CODE    a pairing code from its admin
//	CAFEDESK_E2E_PIN     the starting PIN of a manager of the till's shop
//
// It connects from the setup screen, signs in with the admin's PIN and
// chooses another, sells with options, refunds, voids and closes the day,
// and expects everything sent.
func TestEndToEnd(t *testing.T) {
	server, code, pin := os.Getenv("CAFEDESK_E2E_SERVER"), os.Getenv("CAFEDESK_E2E_CODE"), os.Getenv("CAFEDESK_E2E_PIN")
	if server == "" {
		t.Skip("set CAFEDESK_E2E_SERVER, CAFEDESK_E2E_CODE and CAFEDESK_E2E_PIN to run against a server")
	}
	ctx := context.Background()
	a, _ := newTestAppMode(t, false)
	a.secrets = memSecrets{}
	a.cfg.ServerURL = server
	tt := ui.NewTester(a.view, 1366, 768)
	for _, d := range code {
		mustClick(t, tt, string(d))
	}
	if a.needsSetup() || a.sync.err != "" {
		t.Fatalf("connecting: %+v, sync %+v", a.pairing, a.sync)
	}
	t.Logf("connected as %s of %s; menu of %d items", a.cfg.TerminalCode, a.cfg.ShopLabel, len(a.catalog.Items))

	tt.Frame()
	typePIN(t, tt, pin)
	typePIN(t, tt, "4321")
	typePIN(t, tt, "4321")
	if a.staff == nil || a.staff.MustChangePIN {
		t.Fatalf("signed in as %+v", a.staff)
	}
	a.float.set(money.FromMajor(100))
	a.startDay()
	tt.Frame()

	// Karak with less sugar, and a biryani with extra parotta, in cash.
	mustClick(t, tt, "Karak chai")
	mustClick(t, tt, "Options")
	mustClick(t, tt, "Less sugar")
	mustClick(t, tt, "Done")
	mustClick(t, tt, "Meals")
	mustClick(t, tt, "Chicken biryani")
	mustClick(t, tt, "Options")
	mustClick(t, tt, "Extra parotta  +1.00")
	mustClick(t, tt, "Done")
	a.startPayment()
	a.tendered.set(money.FromMajor(20))
	a.completeSale()
	sold := *a.lastOrder

	// A card sale, voided.
	a.addItem(a.catalog.Items[1])
	a.startPayment()
	a.method = payCard
	a.completeSale()
	a.openOrders()
	a.startVoid()
	a.voiding.reason = "Test order"
	a.confirmVoid()

	// Refund the karak of the first sale.
	a.selectOrder(sold.ID)
	a.startRefund()
	a.refundStep(a.order.Lines[0].ID, 1)
	a.refund.reason = "Other"
	a.confirmRefund()

	// Close the day.
	a.openDayReport()
	a.counted.set(money.FromMajor(100) + 1700 - 100)
	a.closeDay()
	if a.day != nil {
		t.Fatalf("the day didn't close: %s", a.dayErr)
	}
	a.syncNow()
	pending, _ := a.store.PendingCount(ctx)
	if pending != 0 || a.sync.err != "" || a.sync.rejected != "" {
		t.Fatalf("not all sent: %d pending, %+v", pending, a.sync)
	}
	t.Logf("all sent: %+v", a.sync)
}
