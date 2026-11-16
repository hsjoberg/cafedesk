package store_test

import (
	"context"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"

	"github.com/google/uuid"
)

// The demo sales add up in the report as a till's would: every day closed
// and counted but today, which is open, and nothing sold after now.
func TestDemoSales(t *testing.T) {
	st, org := setup(t)
	ctx := context.Background()
	if err := st.SeedDemoMenu(ctx, org); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDemoCosts(ctx, org); err != nil {
		t.Fatal(err)
	}
	shops, _ := st.Shops(ctx, org)
	shop := shops[0].ID
	var people []store.DemoStaff
	for _, name := range []string{"Ravi", "Anil"} {
		role := api.StaffRoleCashier
		if name == "Ravi" {
			role = api.StaffRoleManager
		}
		s, err := st.CreateStaff(ctx, org, api.StaffInput{Name: name, Role: role, ShopIds: []uuid.UUID{shop}, Active: true}, "hash")
		if err != nil {
			t.Fatal(err)
		}
		people = append(people, store.DemoStaff{ID: s.Id, Name: s.Name})
	}
	dubai, _ := time.LoadLocation("Asia/Dubai")
	now := time.Date(2026, 11, 16, 15, 20, 0, 0, dubai)
	if err := st.SeedDemoSales(ctx, org, shop, people[0], people[1:], 10, now, 1); err != nil {
		t.Fatal(err)
	}
	from, to := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	rep, err := st.SalesReport(ctx, org, from, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Orders < 500 || rep.Totals.NetFils <= 0 || rep.Totals.CashFils == 0 || rep.Totals.CardFils == 0 ||
		len(rep.Tills) != 2 || len(rep.Staff) != 1 || len(rep.Items) == 0 {
		t.Errorf("totals %+v, %d tills, %d staff", rep.Totals, len(rep.Tills), len(rep.Staff))
	}
	if rep.Totals.CostFils <= 0 || rep.Totals.ProfitFils <= 0 || rep.Totals.UncostedFils <= 0 ||
		rep.Totals.ProfitFils >= rep.Totals.NetFils {
		t.Errorf("demo profit: %+v", rep.Totals)
	}
	if len(rep.Closes) != 20 {
		t.Fatalf("%d days, want 20", len(rep.Closes))
	}
	for _, c := range rep.Closes {
		if open := c.Date == "2026-11-16"; open != (c.ClosedAt == nil) || open != (c.DifferenceFils == nil) {
			t.Errorf("day %s on %s: closed %v, difference %v", c.Date, c.TerminalCode, c.ClosedAt, c.DifferenceFils)
		}
	}
	today, err := st.SalesReport(ctx, org, to, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range today.Hours {
		if h.Orders > 0 && (h.Hour < 6 || h.Hour > 15) {
			t.Errorf("today, sales at %d:00, before opening or after now", h.Hour)
		}
	}
	if d := rep.Days[len(rep.Days)-1]; d.Orders == 0 || d.Orders > 80 {
		t.Errorf("today, to 15:20: %+v", d)
	}
}
