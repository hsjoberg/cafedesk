package main

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cafedesk/apps/pos/api"
	"cafedesk/pkg/money"

	"github.com/egoist/mygo/ui"
)

func TestMain(m *testing.M) {
	if err := registerFonts(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// printed is what the fake printer received.
type printed struct {
	role string
	doc  PrintDoc
}

type fakePrinter struct {
	jobs []printed
	err  error // every print fails with it while set
}

func (p *fakePrinter) print(cfg Config, role string, doc PrintDoc) error {
	if p.err != nil {
		return p.err
	}
	p.jobs = append(p.jobs, printed{role, doc})
	return nil
}

func (p *fakePrinter) text(role string) string {
	var b strings.Builder
	for _, j := range p.jobs {
		if j.role == role {
			b.WriteString(strings.Join(j.doc.Text(48), "\n") + "\n")
		}
	}
	return b.String()
}

// newTestApp is an app in demo mode, with a database in a temporary
// directory, a fake printer, and nothing signed in.
func newTestApp(t *testing.T) (*app, *fakePrinter) {
	t.Helper()
	return newTestAppMode(t, true)
}

// newTestAppMode is newTestApp, in demo mode or not: not, it has no menu or
// staff until it connects to an admin.
func newTestAppMode(t *testing.T, demo bool) (*app, *fakePrinter) {
	t.Helper()
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.BackupDir = filepath.Join(dir, "backups")
	cfg.ReceiptPrinter = "Receipt"
	a := newApp(cfg, dir)
	a.demo = demo
	s, err := OpenStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	a.store = s
	a.load()
	p := &fakePrinter{}
	a.print = p.print
	a.retryDelays = nil
	return a, p
}

// openTill signs the manager in, past the default PIN, and starts a day
// with a float of 100.
func openTill(t *testing.T, a *app) {
	t.Helper()
	ctx := context.Background()
	st, ok, err := a.store.StaffByPIN(ctx, defaultPIN)
	if !ok || err != nil {
		t.Fatalf("default manager: %v %v", ok, err)
	}
	if err := a.store.SetPIN(ctx, st.ID, "9999"); err != nil {
		t.Fatal(err)
	}
	st.MustChangePIN = false
	a.signIn(st)
	a.float.set(money.FromDirhams(100))
	a.startDay()
	if a.day == nil {
		t.Fatalf("day not started: %s", a.dayErr)
	}
}

func mustClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("click %q: %v (texts: %q)", label, err, tt.Texts())
	}
}

func typePIN(t *testing.T, tt *ui.Tester, pin string) {
	t.Helper()
	for _, d := range pin {
		mustClick(t, tt, string(d))
	}
}

func TestFirstRun(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1366, 768)
	if !tt.HasText("Enter your PIN") {
		t.Fatalf("not locked; texts %q", tt.Texts())
	}
	typePIN(t, tt, "1111")
	if a.staff != nil || !tt.HasText("That PIN doesn't match anyone.") {
		t.Fatalf("a wrong PIN signed in; texts %q", tt.Texts())
	}
	typePIN(t, tt, defaultPIN)
	if !tt.HasText("Choose your own PIN") {
		t.Fatalf("the default PIN wasn't replaced; texts %q", tt.Texts())
	}
	typePIN(t, tt, "1234")
	typePIN(t, tt, "1234")
	if !tt.HasText("choose a PIN other than 1234") {
		t.Fatalf("kept the default PIN; texts %q", tt.Texts())
	}
	typePIN(t, tt, "5678")
	typePIN(t, tt, "5678")
	if !tt.HasText("Start the day") {
		t.Fatalf("no start of day; texts %q", tt.Texts())
	}
	for _, k := range []string{"1", "5", "0"} {
		mustClick(t, tt, k)
	}
	mustClick(t, tt, "Start the day")
	if a.day == nil || a.day.OpeningFloat != 15000 {
		t.Fatalf("day %+v, err %q", a.day, a.dayErr)
	}
	if !tt.HasText("Karak chai") {
		t.Errorf("no order screen; texts %q", tt.Texts())
	}

	// The new PIN signs in; the old one doesn't.
	a.lockTill()
	tt.Frame()
	typePIN(t, tt, "5678")
	if a.staff == nil || a.pinChange != nil {
		t.Errorf("the new PIN didn't sign in")
	}
}

func TestPINLockout(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1366, 768)
	for i := 0; i < maxPINFails; i++ {
		typePIN(t, tt, "0000")
	}
	if !tt.HasText("Too many wrong PINs. Try again in 30 seconds.") {
		t.Fatalf("no lockout; texts %q", tt.Texts())
	}
	a.lockKey("1", tt.Now())
	if a.lock.pin != "" {
		t.Error("a key was taken during the lockout")
	}
}

func TestSaleWithOptions(t *testing.T) {
	a, p := newTestApp(t)
	openTill(t, a)
	tt := ui.NewTester(a.view, 1366, 768)

	mustClick(t, tt, "Karak chai")
	mustClick(t, tt, "Options")
	mustClick(t, tt, "Less sugar")
	mustClick(t, tt, "Done")
	mustClick(t, tt, "Karak chai") // a plain one: a line of its own
	if len(a.cart.Lines) != 2 || a.cart.Lines[0].ModsText() != "Less sugar" {
		t.Fatalf("lines %+v", a.cart.Lines)
	}
	mustClick(t, tt, "Meals")
	mustClick(t, tt, "Chicken biryani")
	mustClick(t, tt, "Options")
	mustClick(t, tt, "Extra parotta  +1.00")
	mustClick(t, tt, "Mild")
	mustClick(t, tt, "Done")
	if got := a.cart.Total(); got != 1700 {
		t.Fatalf("total %s, want 17.00", got)
	}
	if !tt.HasText("Mild, Extra parotta") {
		t.Errorf("modifiers not on the receipt strip; texts %q", tt.Texts())
	}

	mustClick(t, tt, "Charge AED 17.00")
	mustClick(t, tt, "20.00")
	mustClick(t, tt, "Complete sale")
	if !a.cart.Empty() || a.lastChange != 300 {
		t.Fatalf("sale not completed: %q", a.payErr)
	}
	receipt := p.text(roleReceipt)
	for _, want := range []string{"Order T1-001", "Served by Owner", "Less sugar", "Extra parotta", "+1.00", "AED 17.00"} {
		if !strings.Contains(receipt, want) {
			t.Errorf("receipt lacks %q:\n%s", want, receipt)
		}
	}
	if strings.Contains(receipt, "Normal sugar") {
		t.Errorf("receipt prints a default:\n%s", receipt)
	}
	if p.text(roleKitchen) != "" {
		t.Error("a kitchen ticket printed while the kitchen printer is off")
	}

	saved, err := a.store.Order(context.Background(), a.lastOrder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Lines) != 3 || saved.Lines[2].ModsText() != "Mild, Extra parotta" || saved.Lines[2].UnitPrice() != 1500 {
		t.Errorf("saved lines %+v", saved.Lines)
	}
}

func TestKitchenTicket(t *testing.T) {
	a, p := newTestApp(t)
	a.cfg.KitchenEnabled, a.cfg.KitchenPrinter = true, "Kitchen"
	openTill(t, a)
	a.addItem(a.catalog.ItemsIn("tea")[0])
	a.addItem(a.catalog.ItemsIn("meals")[0])
	a.startPayment()
	a.method = payCard
	a.completeSale()
	ticket := p.text(roleKitchen)
	if !strings.Contains(ticket, "Chicken biryani") || strings.Contains(ticket, "Karak") || strings.Contains(ticket, "14.00") {
		t.Errorf("kitchen ticket:\n%s", ticket)
	}
}

func TestFailedPrintShowsRetry(t *testing.T) {
	a, p := newTestApp(t)
	openTill(t, a)
	p.err = errors.New("printer offline")
	a.addItem(a.catalog.Items[0])
	a.startPayment()
	a.method = payCard
	a.completeSale()
	if a.cart.Empty() == false || a.lastCode == "" {
		t.Fatal("a failed print undid the sale")
	}
	if len(a.printNotes) != 1 {
		t.Fatalf("notes %+v", a.printNotes)
	}
	tt := ui.NewTester(a.view, 1366, 768)
	if !tt.HasText("Receipt T1-001 not printed: printer offline") {
		t.Errorf("note not shown; texts %q", tt.Texts())
	}
	p.err = nil
	mustClick(t, tt, "Retry")
	if len(a.printNotes) != 0 || len(p.jobs) != 1 {
		t.Errorf("retry: notes %d, jobs %d", len(a.printNotes), len(p.jobs))
	}
}

func TestVoidNeedsManager(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	ctx := context.Background()
	cashier, err := a.store.AddStaff(ctx, "Ravi", roleCashier, "4321")
	if err != nil {
		t.Fatal(err)
	}
	a.signIn(cashier)
	a.addItem(a.catalog.Items[0])
	a.startPayment()
	a.method = payCard
	a.completeSale()

	tt := ui.NewTester(a.view, 1366, 768)
	mustClick(t, tt, "Ravi")
	mustClick(t, tt, "Orders")
	mustClick(t, tt, "Void")
	mustClick(t, tt, "Test order")
	mustClick(t, tt, "Void T1-001")
	if a.approval == nil || !tt.HasText("Manager PIN needed") {
		t.Fatalf("a cashier voided without a manager; texts %q", tt.Texts())
	}
	typePIN(t, tt, "4321")
	if !tt.HasText("That isn't a manager's PIN.") {
		t.Fatalf("a cashier's PIN approved; texts %q", tt.Texts())
	}
	typePIN(t, tt, "9999")
	if a.order == nil || a.order.Status != statusVoided || a.order.VoidedBy != "Owner" {
		t.Fatalf("not voided: %+v", a.order)
	}
	r, _ := a.store.Report(ctx, a.day.ID)
	if r.Orders != 0 || r.VoidCount != 1 {
		t.Errorf("report %+v", r)
	}
}

func TestRefund(t *testing.T) {
	a, p := newTestApp(t)
	openTill(t, a)
	karak := a.catalog.Items[0]
	a.addItem(karak)
	a.addItem(karak)
	a.startPayment()
	a.tendered.set(money.FromDirhams(5))
	a.completeSale()

	tt := ui.NewTester(a.view, 1366, 768)
	mustClick(t, tt, "Owner")
	mustClick(t, tt, "Orders")
	mustClick(t, tt, "Refund")
	mustClick(t, tt, "+")
	mustClick(t, tt, "Problem with the food or drink")
	mustClick(t, tt, "Refund AED 1.00")
	if a.refund != nil {
		t.Fatalf("not refunded: %+v", a.refund)
	}
	if a.order == nil || a.order.Refunded() != 100 || a.canVoid() {
		t.Fatalf("order after refund %+v", a.order)
	}
	if !strings.Contains(p.text(roleReceipt), "Cash back") {
		t.Errorf("no refund slip:\n%s", p.text(roleReceipt))
	}
	r, _ := a.store.Report(context.Background(), a.day.ID)
	if r.Gross != 200 || r.RefundCash != 100 || r.Net() != 100 || r.ExpectedCash() != 10100 {
		t.Errorf("report %+v", r)
	}
	// Only one is left to refund.
	if _, err := a.store.SaveRefund(context.Background(), a.cfg, *a.order, a.day.ID,
		map[string]int{a.order.Lines[0].ID: 2}, "Other", *a.staff, *a.staff, tt.Now()); err == nil {
		t.Error("refunded more than was left")
	}
}

func TestCloseDay(t *testing.T) {
	a, p := newTestApp(t)
	openTill(t, a)
	a.addItem(a.catalog.ItemsIn("meals")[0])
	a.startPayment()
	a.tendered.set(money.FromDirhams(20))
	a.completeSale() // 14.00 in cash

	tt := ui.NewTester(a.view, 1366, 768)
	mustClick(t, tt, "Owner")
	mustClick(t, tt, "Day report")
	if !tt.HasText("Expected: 114.00") {
		t.Fatalf("expected cash; texts %q", tt.Texts())
	}
	for _, k := range []string{"1", "1", "0"} {
		mustClick(t, tt, k)
	}
	if !tt.HasText("Short by 4.00") {
		t.Errorf("difference not shown; texts %q", tt.Texts())
	}
	mustClick(t, tt, "Close the day")
	if a.day != nil {
		t.Fatalf("day still open: %s", a.dayErr)
	}
	z := p.text(roleReceipt)
	for _, want := range []string{"Day closed", "Net sales", "AED 14.00", "Short", "-4.00", "1 x Chicken biryani"} {
		if !strings.Contains(z, want) {
			t.Errorf("Z report lacks %q:\n%s", want, z)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(a.cfg.BackupDir, "cafedesk-*.db"))
	if len(backups) == 0 {
		t.Error("closing the day made no backup")
	}
	tt.Frame()
	if !tt.HasText("No day open. The next sale starts one.") && !tt.HasText("Tap an item to start an order.") {
		t.Errorf("not back to selling; texts %q", tt.Texts())
	}

	// The next sale starts the next day, with the drawer as counted.
	a.addItem(a.catalog.ItemsIn("meals")[0])
	a.startPayment()
	a.tendered.set(money.FromDirhams(14))
	a.completeSale()
	if a.day == nil || a.day.OpeningFloat != 11000 || a.lastCode != "T1-001" {
		t.Fatalf("next day %+v, order %q, err %q", a.day, a.lastCode, a.payErr)
	}
}

func TestCloseWithoutCounting(t *testing.T) {
	a, p := newTestApp(t)
	openTill(t, a)
	a.addItem(a.catalog.ItemsIn("meals")[0])
	a.startPayment()
	a.tendered.set(money.FromDirhams(14))
	a.completeSale() // 14.00 in cash

	tt := ui.NewTester(a.view, 1366, 768)
	mustClick(t, tt, "Owner")
	mustClick(t, tt, "Day report")
	mustClick(t, tt, "Close without counting")
	if a.day != nil {
		t.Fatalf("day still open: %s", a.dayErr)
	}
	if z := p.text(roleReceipt); !strings.Contains(z, "not counted") || strings.Contains(z, "Short") {
		t.Errorf("Z report:\n%s", z)
	}
	// What the drawer should hold carries over to the next day.
	if a.carry != 11400 {
		t.Errorf("carry %s", a.carry)
	}
	a.addItem(a.catalog.Items[0])
	a.startPayment()
	a.method = payCard
	a.completeSale()
	if a.day == nil || a.day.OpeningFloat != 11400 {
		t.Fatalf("next day %+v, err %q", a.day, a.payErr)
	}
}

func TestCashInOrOut(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	tt := ui.NewTester(a.view, 1366, 768)
	mustClick(t, tt, "Owner")
	mustClick(t, tt, "Day report")
	mustClick(t, tt, "Cash in or out")
	mustClick(t, tt, "Take out")
	for _, k := range []string{"4", "0"} {
		mustClick(t, tt, k)
	}
	mustClick(t, tt, "Take out AED 40.00")
	if !tt.HasText("Choose a reason.") {
		t.Fatalf("saved without a reason; texts %q", tt.Texts())
	}
	mustClick(t, tt, "Banked")
	mustClick(t, tt, "Take out AED 40.00")
	if a.cashMove != nil {
		t.Fatalf("not saved: %s", a.cashMove.err)
	}
	tt.Frame()
	if !tt.HasText("Taken out, Banked") || !tt.HasText("Expected: 60.00") {
		t.Errorf("not on the report; texts %q", tt.Texts())
	}
	events, _ := a.store.PendingEvents(context.Background(), 100)
	last := events[len(events)-1]
	if last.CashMovement == nil || last.CashMovement.AmountFils != -4000 || last.CashMovement.Reason != "Banked" ||
		last.CashMovement.ApprovedByName != "Owner" {
		t.Errorf("sent %+v", last)
	}
}

func TestDayEndsByItself(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	ctx := context.Background()
	mgr := *a.staff
	// A day opened at 10:00 two days ago, still open: its end of day passed.
	a.store.CloseDay(ctx, a.day.ID, a.staff, nil, a.day.OpenedAt)
	y := time.Now().AddDate(0, 0, -2)
	opened := time.Date(y.Year(), y.Month(), y.Day(), 10, 0, 0, 0, time.Local)
	day, err := a.store.StartDay(ctx, a.cfg, *a.staff, 5000, opened)
	if err != nil {
		t.Fatal(err)
	}
	a.day = &day
	a.cfg.DayEnds = "04:00"
	a.checkDayEnd()
	if a.day != nil || a.staff != nil {
		t.Fatalf("still open %+v, signed in %+v", a.day, a.staff)
	}
	got, _ := a.store.Day(ctx, day.ID)
	want := opened.AddDate(0, 0, 1).Add(-6 * time.Hour) // 04:00 the next day
	if !got.Auto() || got.Counted || !got.ClosedAt.Equal(want) {
		t.Errorf("closed %+v, want at %s, by itself, uncounted", got, want)
	}
	if a.carry != 5000 {
		t.Errorf("carry %s", a.carry)
	}
	// It goes to the admin closed, by no one, uncounted.
	events, err := a.store.PendingEvents(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	var sent *api.BusinessDayEvent
	for _, ev := range events {
		if ev.BusinessDay != nil && ev.BusinessDay.Id.String() == day.ID {
			sent = ev.BusinessDay
		}
	}
	if sent == nil || sent.ClosedAt == nil || sent.ClosedBy != nil || sent.ClosedByName != nil || sent.CountedCashFils != nil {
		t.Errorf("sent %+v", sent)
	}

	// Never: a day stays open.
	a.cfg.DayEnds = dayEndsOff
	if _, err := a.store.StartDay(ctx, a.cfg, mgr, 0, opened); err != nil {
		t.Fatal(err)
	}
	if r, err := a.store.CloseIfOver(ctx, a.cfg.dayEnds(), time.Now()); r != nil || err != nil {
		t.Errorf("closed with no end of day: %v", err)
	}
	if _, open, _ := a.store.OpenDay(ctx); !open {
		t.Error("the day didn't stay open")
	}
}

func TestDayEndAfter(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		opened time.Time
		want   time.Time
	}{
		{at(17, 7, 10), at(18, 4, 0)},  // a day from the morning
		{at(17, 23, 30), at(18, 4, 0)}, // opened late
		{at(18, 2, 0), at(18, 4, 0)},   // after midnight, before the end
		{at(18, 4, 0), at(19, 4, 0)},   // at the end exactly: the next one
	} {
		if got, ok := dayEndAfter(c.opened, "04:00"); !ok || !got.Equal(c.want) {
			t.Errorf("opened %s: %s, want %s", c.opened, got, c.want)
		}
	}
	if _, ok := dayEndAfter(at(17, 7, 0), dayEndsOff); ok {
		t.Error("an end of day when off")
	}
}

func TestSettingsNeedManager(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	cashier, _ := a.store.AddStaff(context.Background(), "Ravi", roleCashier, "4321")
	a.signIn(cashier)
	a.openSettings()
	if a.screen == screenSettings || a.approval == nil {
		t.Fatal("a cashier opened Settings")
	}
	tt := ui.NewTester(a.view, 1366, 768)
	typePIN(t, tt, "9999")
	if a.screen != screenSettings || !tt.HasText("Opened by Owner") {
		t.Fatalf("settings not open; texts %q", tt.Texts())
	}

	// Add a person on the on-screen keyboard.
	a.settingsDrag.state.Y = math.MaxFloat32
	tt.Frame()
	mustClick(t, tt, "Add a person")
	for _, k := range []string{"A", "n", "i", "l"} {
		mustClick(t, tt, k)
	}
	mustClick(t, tt, "Done")
	typePIN(t, tt, "2468")
	typePIN(t, tt, "2468")
	if !tt.HasText("Anil can now sign in with their PIN.") {
		t.Fatalf("not added; texts %q", tt.Texts())
	}
	st, ok, _ := a.store.StaffByPIN(context.Background(), "2468")
	if !ok || st.Name != "Anil" || st.IsManager() {
		t.Errorf("added %+v", st)
	}
}

func TestAmountEntry(t *testing.T) {
	var e amountEntry
	for _, k := range []string{"1", "2", ".", "5", "0", "9", "back", "back"} {
		e.press(k)
	}
	if e.text != "12." {
		t.Errorf("text %q, want %q", e.text, "12.")
	}
	e.set(money.FromDirhams(50))
	e.press("7")
	if e.text != "7" {
		t.Errorf("a key after a quick amount gave %q", e.text)
	}
}

func TestQuickAmounts(t *testing.T) {
	got := quickAmounts(1600)
	want := []money.Fils{1600, 2000, 5000, 10000}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
