package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func openTestStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := OpenStore(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCatalogSeedAndReload(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	c, err := s.LoadCatalog(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	demo := demoCatalog()
	if len(c.Categories) != len(demo.Categories) || len(c.Items) != len(demo.Items) || len(c.Groups) != len(demo.Groups) {
		t.Fatalf("loaded %d categories, %d items, %d groups", len(c.Categories), len(c.Items), len(c.Groups))
	}
	karak := c.ItemsIn("tea")[0]
	if karak.ID != "karak" || karak.Kitchen || len(karak.Groups) != 1 {
		t.Errorf("karak %+v", karak)
	}
	if mods := c.DefaultMods(karak); len(mods) != 1 || mods[0].ID != "sugar-normal" {
		t.Errorf("default mods %+v", mods)
	}
	if !c.ItemsIn("meals")[0].Kitchen {
		t.Error("meals don't go to the kitchen")
	}
	if g := c.Groups["extras"]; !g.Multiple || len(g.Modifiers) != 3 || g.Modifiers[0].Delta != 100 {
		t.Errorf("extras %+v", g)
	}
	// Loading again doesn't seed twice.
	c2, _ := s.LoadCatalog(ctx, true)
	if empty, _ := openTestStore(t, t.TempDir()).LoadCatalog(ctx, false); len(empty.Categories) != 0 {
		t.Error("a till out of demo mode got the demo menu")
	}
	if len(c2.Items) != len(c.Items) {
		t.Errorf("reloaded %d items", len(c2.Items))
	}
}

func TestStaffPINs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	if err := s.EnsureStaff(ctx); err != nil {
		t.Fatal(err)
	}
	mgr, ok, _ := s.StaffByPIN(ctx, defaultPIN)
	if !ok || !mgr.IsManager() || !mgr.MustChangePIN {
		t.Fatalf("default manager %+v", mgr)
	}
	if err := s.SetPIN(ctx, mgr.ID, defaultPIN); err == nil {
		t.Error("kept the default PIN")
	}
	if err := s.SetPIN(ctx, mgr.ID, "12a4"); err == nil {
		t.Error("took a PIN with a letter")
	}
	if err := s.SetPIN(ctx, mgr.ID, "8080"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.StaffByPIN(ctx, defaultPIN); ok {
		t.Error("the old PIN still signs in")
	}
	if _, err := s.AddStaff(ctx, "Ravi", roleCashier, "8080"); err != errPINTaken {
		t.Errorf("a taken PIN: %v", err)
	}
	ravi, err := s.AddStaff(ctx, "Ravi", roleCashier, "3030")
	if err != nil {
		t.Fatal(err)
	}
	// The last manager can't be demoted or switched off.
	mgr.Role = roleCashier
	if err := s.UpdateStaff(ctx, mgr); err == nil {
		t.Error("demoted the last manager")
	}
	ravi.Role = roleManager
	if err := s.UpdateStaff(ctx, ravi); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateStaff(ctx, mgr); err != nil {
		t.Errorf("demoting one of two managers: %v", err)
	}
	ravi.Active = false
	if err := s.UpdateStaff(ctx, ravi); err == nil {
		t.Error("switched off the last manager")
	}
	// The hash isn't the PIN.
	rows, _ := s.query(ctx, "SELECT pin_hash FROM staff")
	for _, r := range rows.Values {
		if h := str(r[0]); h == "3030" || h == "8080" || !checkPIN(h, "3030") && !checkPIN(h, "8080") {
			t.Errorf("hash %q", h)
		}
	}
}

func TestDaysAndOrders(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	s.EnsureStaff(ctx)
	mgr, _, _ := s.StaffByPIN(ctx, defaultPIN)
	cfg := defaultConfig()
	now := time.Now()

	day, err := s.StartDay(ctx, cfg, mgr, 5000, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartDay(ctx, cfg, mgr, 0, now); err == nil {
		t.Error("started a second day")
	}
	n, _ := s.NextOrderNumber(ctx, day.ID)
	c := demoCatalog()
	o := Order{ID: newID(), OrgID: "org", ShopID: "shop", TerminalID: "T1", DayID: day.ID, Number: n,
		Code: orderCode("T1", n), CreatedAt: now, StaffID: mgr.ID, Method: payCash, Tendered: 1000, Change: 600,
		Lines: []Line{{Item: c.Items[0], Qty: 2, Mods: c.DefaultMods(c.Items[0])}}}
	o.Total = o.Lines[0].Total()
	if err := s.SaveOrder(ctx, &o); err != nil {
		t.Fatal(err)
	}
	// The same number again breaks the unique constraint, and nothing of
	// the order is written.
	dup := o
	dup.ID = newID()
	dup.Lines = append([]Line(nil), o.Lines...)
	if err := s.SaveOrder(ctx, &dup); err == nil {
		t.Error("saved a duplicate number")
	}
	if n, _ := s.NextOrderNumber(ctx, day.ID); n != 2 {
		t.Errorf("next number %d", n)
	}
	got, err := s.Order(ctx, o.ID)
	if err != nil || got.Total != 200 || len(got.Lines) != 1 || len(got.Lines[0].Mods) != 1 || got.StaffName != "Owner" {
		t.Fatalf("order %+v, %v", got, err)
	}

	closed, err := s.CloseDay(ctx, day.ID, mgr, 5100, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if closed.ExpectedCash() != 5200 || closed.Difference() != -100 {
		t.Errorf("expected %s, difference %s", closed.ExpectedCash(), closed.Difference())
	}
	if _, open, _ := s.OpenDay(ctx); open {
		t.Error("the day is still open")
	}
	if err := s.VoidOrder(ctx, o.ID, day.ID, mgr, "Test order", now); err == nil {
		t.Error("voided an order of a closed day")
	}
	st, _ := s.Stats(ctx)
	if st.Orders != 1 || st.Unsent != 3 { // the day opened, the order, the day closed
		t.Errorf("stats %+v", st)
	}
}

func TestPhase0DatabaseSetAside(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "pos.db")
	old, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	old.Execute(ctx, "CREATE TABLE orders (id TEXT PRIMARY KEY, business_day TEXT)")
	old.Execute(ctx, "INSERT INTO orders VALUES ('x', '2026-11-16')")
	old.Close()

	s := openTestStore(t, dir)
	if st, err := s.Stats(ctx); err != nil || st.Orders != 0 {
		t.Errorf("new database %+v, %v", st, err)
	}
	aside, _ := filepath.Glob(filepath.Join(dir, "pos-phase0-*.db"))
	if len(aside) != 1 {
		t.Errorf("phase 0 database set aside as %v", aside)
	}
}

func TestReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenStore(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	s.EnsureStaff(ctx)
	n, _ := s.WriteCheck(ctx)
	s.Close()
	s2 := openTestStore(t, dir)
	if m, _ := s2.WriteCheck(ctx); m != n+1 {
		t.Errorf("after reopening, %d check rows, want %d", m, n+1)
	}
	if staff, _ := s2.AllStaff(ctx); len(staff) != 1 {
		t.Errorf("staff %+v", staff)
	}
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	s.EnsureStaff(ctx)
	dir := filepath.Join(t.TempDir(), "backups")
	start := time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local)
	for i := 0; i < backupsKept+2; i++ {
		if _, err := s.Backup(ctx, dir, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "cafedesk-*.db"))
	if len(files) != backupsKept {
		t.Errorf("%d backups kept, want %d", len(files), backupsKept)
	}
	// A backup is a whole database on its own.
	last, path := s.LastBackup(ctx)
	if last.IsZero() || path == "" {
		t.Fatal("last backup not recorded")
	}
	copyDir := t.TempDir()
	data, _ := os.ReadFile(path)
	os.WriteFile(filepath.Join(copyDir, "pos.db"), data, 0o644)
	restored := openTestStore(t, copyDir)
	if staff, err := restored.AllStaff(ctx); err != nil || len(staff) != 1 {
		t.Errorf("restored staff %+v, %v", staff, err)
	}
}

func TestOrderCode(t *testing.T) {
	if got := orderCode("T1", 12); got != "T1-012" {
		t.Errorf("orderCode = %q", got)
	}
}
