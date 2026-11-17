package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type screen int

const (
	screenOrder screen = iota
	screenOrders
	screenDay
	screenSettings
	screenChecks
)

// sheet is what covers the menu wall of the order screen.
type sheet int

const (
	sheetNone sheet = iota
	sheetPay
	sheetMenu
	sheetOptions
)

// app is the terminal's state. The view builds the window from it on the
// main thread; work that may block (the database, printing, the network)
// runs through async and comes back to the main thread to change it.
type app struct {
	cfg    Config
	cfgDir string
	cfgErr string
	theme  *ui.Theme

	store    *Store
	storeErr error
	catalog  *Catalog

	win *mygo.Window // nil in tests
	// async runs work off the main thread, then the function it returns on
	// the main thread. Tests run both at once.
	async func(work func() func())
	// print sends a document to a role's printer; tests replace it.
	print       func(cfg Config, role string, doc PrintDoc) error
	retryDelays []time.Duration // between attempts at a kitchen ticket
	// The system's start-at-login setting, nil in tests.
	setOpenAtLogin func(bool) error
	openAtLogin    bool
	// demo runs the till without an admin, on the demo menu and its own
	// staff, for trying it out and for tests (CAFEDESK_DEMO=1). Otherwise a
	// till is connected to an admin before its first sale.
	demo bool
	// Where the device token is kept; nil leaves the till unconnected.
	secrets secretStore
	token   string
	synced  bool // the menu and staff have come from the admin at least once
	sync    syncState
	pairing *pairState

	staff *Staff       // signed in; nil while locked
	day   *BusinessDay // the open day; nil until the next sale starts one
	// firstDay is a till that has never had a day: its first starts with a
	// count of the drawer. After that, a day starts with the next sale,
	// from what the drawer carries over.
	firstDay bool
	carry    money.Fils // what the drawer should hold while no day is open
	closing  bool       // the day is closing by itself

	screen   screen
	sheet    sheet
	category string
	page     int

	cart        Cart
	selected    int // the receipt line showing its controls, or -1
	optionsLine int // the line the options sheet edits
	nextNumber  int

	method   string
	tendered amountEntry
	saving   bool
	payErr   string

	lastCode    string
	lastChange  money.Fils
	lastOrder   *Order
	printStatus string
	printNotes  []printNote
	noteSeq     int

	lock      lockState
	approval  *approval
	pinChange *pinChange
	keyboard  *keyboard

	float    amountEntry // starting the till's first day
	counted  amountEntry // closing the day
	dayErr   string
	report   *DayReport
	cashMove *cashMoveState

	orders    []OrderSummary
	ordersErr string
	order     *Order // selected on the Orders screen
	refund    *refundState
	voiding   *voidState

	settings settingsState

	// Finger drags of the scrolling lists (see touchscroll.go).
	linesDrag      touchDrag
	checksDrag     touchDrag
	scrollTestDrag touchDrag
	ordersDrag     touchDrag
	reportDrag     touchDrag
	settingsDrag   touchDrag

	checks checkState
}

// checkState is what the Checks screen found.
type checkState struct {
	maxFingers int
	fingersNow int
	scrollOne  bool
	arabicOK   bool

	db    result
	srv   result
	login result

	printers       []string
	defaultPrinter string
	printersLoaded bool
}

// result is the outcome of an action, shown under it.
type result struct {
	text string
	err  bool
	busy bool
}

func errResult(prefix string, err error) result { return result{text: prefix + err.Error(), err: true} }

// printNote is a print that failed, shown at the top of the receipt strip
// until it is retried or dismissed.
type printNote struct {
	id    int
	role  string
	label string // "Kitchen ticket T1-004"
	doc   PrintDoc
	err   string
}

func newApp(cfg Config, cfgDir string) *app {
	money.Use(cfg.currency())
	return &app{
		cfg:         cfg,
		cfgDir:      cfgDir,
		theme:       posTheme(),
		catalog:     &Catalog{Groups: map[string]ModifierGroup{}},
		selected:    -1,
		optionsLine: -1,
		nextNumber:  1,
		method:      payCash,
		async:       runInline,
		print:       sendToPrinter,
		retryDelays: []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second},
	}
}

func runInline(work func() func()) {
	if done := work(); done != nil {
		done()
	}
}

// sendToPrinter prints a document on the printer of a role, in its mode.
func sendToPrinter(cfg Config, role string, doc PrintDoc) error {
	name, mode := cfg.printer(role)
	if name == "" {
		if role == roleKitchen {
			return errors.New("no kitchen printer is chosen. Choose one in Settings")
		}
		return errors.New("no receipt printer is chosen. Choose one in Settings")
	}
	if mode == printWindows {
		if doc.Image != nil {
			return printImage(name, doc.Title, doc.Image)
		}
		return printText(name, doc.Title, doc.Lines)
	}
	return printRaw(name, doc.Title, doc.ESCPOS())
}

// attach connects the app to its window: background work then reports
// back to the main thread through it.
func (a *app) attach(win *mygo.Window) {
	a.win = win
	a.async = func(work func() func()) {
		go func() {
			if done := work(); done != nil {
				win.Update(done)
			}
		}()
	}
	a.setOpenAtLogin = mygo.App.SetOpenAtLogin
	a.openAtLogin = mygo.App.OpenAtLogin()
	go func() {
		for range time.Tick(15 * time.Minute) {
			win.Update(a.autoBackup)
		}
	}()
	go func() {
		for range time.Tick(30 * time.Second) {
			mygo.RunOnMain(a.syncNow)
		}
	}()
	go func() {
		for range time.Tick(time.Minute) {
			mygo.RunOnMain(a.checkDayEnd)
		}
	}()
}

// load reads the menu and makes sure someone can sign in, as the app starts.
func (a *app) load() {
	if a.store == nil {
		return
	}
	ctx := context.Background()
	if a.demo {
		if err := a.store.EnsureStaff(ctx); err != nil {
			a.storeErr = err
			return
		}
	} else if v, err := a.store.SyncVersion(ctx); err == nil {
		a.synced = v > 0
	}
	c, err := a.store.LoadCatalog(ctx, a.demo)
	if err != nil {
		a.storeErr = fmt.Errorf("read the menu: %w", err)
		return
	}
	a.setCatalog(c)
}

// setCatalog puts a menu on the screen, keeping the chosen category if it
// is still there.
func (a *app) setCatalog(c *Catalog) {
	a.catalog = c
	if _, ok := c.Category(a.category); !ok {
		a.category, a.page = "", 0
		if len(c.Categories) > 0 {
			a.category = c.Categories[0].ID
		}
	}
}

// needsSetup reports whether the till must be connected to an admin before
// it can be used: it has never been, or hasn't yet fetched the menu and staff.
func (a *app) needsSetup() bool {
	return !a.demo && (a.token == "" || a.cfg.ShopID == "local" || !a.synced)
}

func (a *app) storeError() error {
	if a.storeErr != nil {
		return a.storeErr
	}
	return errors.New("the database is not open")
}

func (a *app) currentCode() string { return orderCode(a.cfg.TerminalCode, a.nextNumber) }

// Signing in and out.

// lockState is the PIN being typed on the lock screen.
type lockState struct {
	pin     string
	err     string
	fails   int
	until   time.Time // no PINs are taken until then, after too many wrong ones
	checks  bool      // a PIN is being checked
	shakeAt time.Time
}

const (
	maxPINFails = 5
	pinLockout  = 30 * time.Second
)

// lockKey applies a key of the lock screen's numpad.
func (a *app) lockKey(k string, now time.Time) {
	l := &a.lock
	if l.checks || now.Before(l.until) {
		return
	}
	l.err = ""
	switch k {
	case "back":
		if l.pin != "" {
			l.pin = l.pin[:len(l.pin)-1]
		}
	case "clear":
		l.pin = ""
	default:
		if len(l.pin) < pinLength {
			l.pin += k
		}
	}
	if len(l.pin) == pinLength {
		a.unlock(l.pin, now)
	}
}

func (a *app) unlock(pin string, now time.Time) {
	store, storeErr := a.store, a.storeError()
	a.lock.checks = true
	a.async(func() func() {
		if store == nil {
			return func() { a.lock.checks, a.lock.pin, a.lock.err = false, "", storeErr.Error() }
		}
		st, ok, err := store.StaffByPIN(context.Background(), pin)
		return func() {
			l := &a.lock
			l.checks, l.pin = false, ""
			switch {
			case err != nil:
				l.err = "Couldn't check the PIN: " + err.Error()
			case !ok:
				l.fails++
				l.shakeAt = now
				l.err = "That PIN doesn't match anyone."
				if l.fails >= maxPINFails {
					l.until = now.Add(pinLockout)
					l.fails = 0
					l.err = "Too many wrong PINs. Try again in 30 seconds."
				}
			default:
				l.fails, l.err = 0, ""
				a.signIn(st)
			}
		}
	})
}

func (a *app) signIn(st Staff) {
	a.staff = &st
	a.screen, a.sheet = screenOrder, sheetNone
	if st.MustChangePIN {
		a.pinChange = &pinChange{staffID: st.ID, name: st.Name, forced: true}
	}
	a.loadDay()
	a.autoBackup()
}

func (a *app) lockTill() {
	a.staff = nil
	a.sheet = sheetNone
	a.screen = screenOrder
	a.approval, a.keyboard, a.pinChange, a.refund, a.voiding, a.pairing = nil, nil, nil, nil, nil, nil
	a.settings.by = nil
}

// loadDay reads the open day, and the next order number in it, first
// closing it if its end of day has passed. With no day open, it reads what
// the drawer carries over to the next.
func (a *app) loadDay() {
	store, ends := a.store, a.cfg.dayEnds()
	if store == nil {
		return
	}
	a.async(func() func() {
		ctx := context.Background()
		_, err := store.CloseIfOver(ctx, ends, time.Now())
		var day BusinessDay
		var open, had bool
		var carry money.Fils
		next := 1
		if err == nil {
			day, open, err = store.OpenDay(ctx)
		}
		if err == nil && open {
			if n, nerr := store.NextOrderNumber(ctx, day.ID); nerr == nil {
				next = n
			}
		} else if err == nil {
			carry, had, err = store.CarryOver(ctx)
		}
		return func() {
			if err != nil {
				a.dayErr = "Couldn't read the day: " + err.Error()
				return
			}
			a.day = nil
			if open {
				a.day = &day
			}
			a.firstDay = !open && !had
			a.carry = carry
			a.nextNumber = next
		}
	})
}

// checkDayEnd closes the open day by itself once its end of day has
// passed, without a count, and locks the till: the next person to sign in
// starts afresh, and the next sale starts the next day.
func (a *app) checkDayEnd() {
	if a.day == nil || a.store == nil || a.closing || a.saving {
		return
	}
	end, ok := dayEndAfter(a.day.OpenedAt, a.cfg.dayEnds())
	if !ok || time.Now().Before(end) {
		return
	}
	store, ends := a.store, a.cfg.dayEnds()
	a.closing = true
	a.async(func() func() {
		r, err := store.CloseIfOver(context.Background(), ends, time.Now())
		return func() {
			a.closing = false
			if err != nil {
				a.dayErr = "The day didn't close by itself: " + err.Error()
				return
			}
			if r != nil {
				a.dayClosed(*r)
				a.lockTill()
			}
		}
	})
}

// dayClosed clears the screen of a day that closed, by hand or by itself.
func (a *app) dayClosed(r DayReport) {
	a.backupNow()
	a.syncNow()
	a.day, a.report = nil, nil
	a.carry = r.ExpectedCash()
	if r.Day.Counted {
		a.carry = r.Day.CountedCash
	}
	a.nextNumber = 1
	a.counted = amountEntry{}
	a.screen = screenOrder
	a.lastCode, a.lastOrder = "", nil
}

// startDay opens the till's first day with the cash counted in the drawer.
func (a *app) startDay() {
	if a.store == nil || a.staff == nil {
		a.dayErr = a.storeError().Error()
		return
	}
	store, cfg, by, float := a.store, a.cfg, *a.staff, a.float.value()
	a.async(func() func() {
		d, err := store.StartDay(context.Background(), cfg, by, float, time.Now())
		return func() {
			if err != nil {
				a.dayErr = "The day didn't start: " + err.Error()
				return
			}
			a.day, a.dayErr, a.nextNumber, a.firstDay = &d, "", 1, false
			a.float = amountEntry{}
			a.syncNow()
		}
	})
}

// Manager approval.

// approval asks for a manager's PIN before an action.
type approval struct {
	what   string // "Void T1-004"
	pin    string
	err    string
	checks bool
	then   func(manager Staff)
}

// withManager runs then with the signed-in manager, or once a manager has
// entered their PIN.
func (a *app) withManager(what string, then func(manager Staff)) {
	if a.staff != nil && a.staff.IsManager() {
		then(*a.staff)
		return
	}
	a.approval = &approval{what: what, then: then}
}

func (a *app) approvalKey(k string) {
	ap := a.approval
	if ap == nil || ap.checks {
		return
	}
	ap.err = ""
	switch k {
	case "back":
		if ap.pin != "" {
			ap.pin = ap.pin[:len(ap.pin)-1]
		}
	case "clear":
		ap.pin = ""
	default:
		if len(ap.pin) < pinLength {
			ap.pin += k
		}
	}
	if len(ap.pin) < pinLength {
		return
	}
	store, pin := a.store, ap.pin
	ap.checks = true
	a.async(func() func() {
		var st Staff
		var ok bool
		var err error
		if store != nil {
			st, ok, err = store.StaffByPIN(context.Background(), pin)
		}
		return func() {
			ap.checks, ap.pin = false, ""
			switch {
			case err != nil:
				ap.err = err.Error()
			case !ok || !st.IsManager():
				ap.err = "That isn't a manager's PIN."
			default:
				a.approval = nil
				ap.then(st)
			}
		}
	})
}

// PINs.

// pinChange is choosing a new PIN: typed twice.
type pinChange struct {
	staffID string
	name    string
	forced  bool // the default PIN: no way around it
	first   string
	pin     string
	err     string
}

func (a *app) pinChangeKey(k string) {
	p := a.pinChange
	if p == nil {
		return
	}
	p.err = ""
	switch k {
	case "back":
		if p.pin != "" {
			p.pin = p.pin[:len(p.pin)-1]
		}
	case "clear":
		p.pin = ""
	default:
		if len(p.pin) < pinLength {
			p.pin += k
		}
	}
	if len(p.pin) < pinLength {
		return
	}
	if p.first == "" {
		p.first, p.pin = p.pin, ""
		return
	}
	if p.pin != p.first {
		p.first, p.pin, p.err = "", "", "The two PINs were different. Start again."
		return
	}
	if p.staffID == "" { // a new person: Settings adds them with it
		a.pinChange = nil
		a.staffPINChosen(p.pin)
		return
	}
	store, id, pin := a.store, p.staffID, p.pin
	a.async(func() func() {
		err := errors.New("the database is not open")
		if store != nil {
			err = store.SetPIN(context.Background(), id, pin)
		}
		return func() {
			if err != nil {
				p.first, p.pin, p.err = "", "", err.Error()
				return
			}
			a.pinChange = nil
			if a.staff != nil && a.staff.ID == id {
				a.staff.MustChangePIN = false
			}
			a.syncNow()
			a.settings.msg = result{text: "PIN changed for " + p.name + "."}
		}
	})
}

// Amounts typed on a numpad.

// amountEntry is an amount being typed in whole units of the currency,
// such as dirhams.
type amountEntry struct {
	text  string
	quick bool // set by a quick button: the next key starts afresh
}

func (e *amountEntry) value() money.Fils {
	f, err := money.Parse(e.text)
	if err != nil {
		return 0
	}
	return f
}

func (e *amountEntry) set(f money.Fils) { e.text, e.quick = f.String(), true }

// press applies a numpad key: a digit, "." or "back".
func (e *amountEntry) press(k string) {
	if e.quick {
		e.text, e.quick = "", false
		if k == "back" {
			return
		}
	}
	switch k {
	case "back":
		if e.text != "" {
			e.text = e.text[:len(e.text)-1]
		}
	case ".":
		if !strings.Contains(e.text, ".") && money.Current().Decimals > 0 {
			if e.text == "" {
				e.text = "0"
			}
			e.text += "."
		}
	default:
		if _, frac, ok := strings.Cut(e.text, "."); ok && len(frac) >= money.Current().Decimals {
			return
		}
		if len(e.text) >= 7 {
			return
		}
		if e.text == "0" {
			e.text = ""
		}
		e.text += k
	}
}

// The order.

func (a *app) addItem(it Item) {
	n := len(a.cart.Lines)
	a.selected = a.cart.Add(it, a.catalog.DefaultMods(it))
	a.lastCode = ""
	if len(a.cart.Lines) > n {
		a.linesDrag.state.Y = math.MaxFloat32 // a new line: show the end of the receipt
	}
}

func (a *app) clearOrder() {
	a.cart.Clear()
	a.selected = -1
}

func (a *app) openOptions(i int) {
	a.optionsLine = i
	a.sheet = sheetOptions
}

// chooseModifier sets a modifier of the options line: the one of a group
// that takes one, or toggles one of a group that takes several.
func (a *app) chooseModifier(g ModifierGroup, m Modifier) {
	i := a.optionsLine
	if i < 0 || i >= len(a.cart.Lines) {
		return
	}
	mods := a.cart.Lines[i].Mods
	var out []LineMod
	had := false
	for _, lm := range mods {
		if lm.GroupID == g.ID && (!g.Multiple || lm.ID == m.ID) {
			had = had || lm.ID == m.ID
			continue
		}
		out = append(out, lm)
	}
	if !g.Multiple || !had {
		out = append(out, lineMod(g, m))
	}
	a.cart.SetMods(i, a.orderMods(a.cart.Lines[i].Item, out))
}

// orderMods sorts a line's modifiers in the order of the item's groups and
// of the modifiers in them, as they print.
func (a *app) orderMods(it Item, mods []LineMod) []LineMod {
	var out []LineMod
	for _, g := range a.catalog.GroupsOf(it) {
		for _, m := range g.Modifiers {
			for _, lm := range mods {
				if lm.GroupID == g.ID && lm.ID == m.ID {
					out = append(out, lm)
				}
			}
		}
	}
	return out
}

// Payment.

func (a *app) startPayment() {
	if a.cart.Empty() {
		return
	}
	a.sheet = sheetPay
	a.method = payCash
	a.tendered = amountEntry{}
	a.payErr = ""
	a.selected = -1
}

func (a *app) cancelPayment() {
	if !a.saving {
		a.sheet = sheetNone
	}
}

func (a *app) canComplete() bool {
	if a.saving || a.cart.Empty() || a.firstDay || a.staff == nil {
		return false
	}
	return a.method == payCard || a.tendered.value() >= a.cart.Total()
}

// quickAmounts are the cash buttons: the exact amount, then the next three
// round amounts above it, as notes go: 5, 10, 20, 50, 100 and so on, in
// whole units of the currency.
func quickAmounts(total money.Fils) []money.Fils {
	out := []money.Fils{total}
	for step := int64(1); len(out) < 4 && step <= 1_000_000_000; step *= 10 {
		for _, n := range []int64{1, 2, 5} {
			if f := money.FromMajor(n * step); f > total && len(out) < 4 {
				out = append(out, f)
			}
		}
	}
	return out
}

// completeSale saves the order, then clears the screen and prints. A
// failed save keeps the order on screen; a failed print never undoes a
// sale.
func (a *app) completeSale() {
	if !a.canComplete() {
		return
	}
	now := time.Now()
	o := Order{
		ID:         newID(),
		OrgID:      a.cfg.OrgID,
		ShopID:     a.cfg.ShopID,
		TerminalID: a.cfg.TerminalID,
		Number:     a.nextNumber,
		Code:       a.currentCode(),
		CreatedAt:  now,
		StaffID:    a.staff.ID,
		StaffName:  a.staff.Name,
		Lines:      append([]Line(nil), a.cart.Lines...),
		Total:      a.cart.Total(),
		Method:     a.method,
		Status:     statusCompleted,
	}
	if o.Method == payCash {
		o.Tendered = a.tendered.value()
		o.Change = o.Tendered - o.Total
	} else {
		o.Tendered = o.Total
	}
	a.saving = true
	a.payErr = ""
	store, storeErr, cfg, by := a.store, a.storeError(), a.cfg, *a.staff
	seen := "" // the day the screen shows, whose next number it has
	if a.day != nil {
		seen = a.day.ID
	}
	a.async(func() func() {
		ctx := context.Background()
		err := storeErr
		var day BusinessDay
		next := o.Number + 1
		if store != nil {
			// The first sale after a close starts the next day.
			day, _, err = store.EnsureDay(ctx, cfg, by, now)
			if err == nil && day.ID != seen {
				var n int
				if n, err = store.NextOrderNumber(ctx, day.ID); err == nil {
					o.Number, o.Code = n, orderCode(cfg.TerminalCode, n)
				}
			}
			if err == nil {
				o.DayID, o.DayDate = day.ID, day.Date
				err = store.SaveOrder(ctx, &o)
			}
			if n, nerr := store.NextOrderNumber(ctx, o.DayID); err == nil && nerr == nil {
				next = n
			}
		}
		return func() {
			a.saving = false
			if err != nil {
				a.payErr = "Order not saved: " + err.Error()
				return
			}
			if day.ID != "" {
				a.day = &day
			}
			a.sheet = sheetNone
			a.clearOrder()
			a.nextNumber = next
			a.lastOrder = &o
			a.lastCode = o.Code
			a.lastChange = o.Change
			a.printOrder(o, false)
			a.syncNow()
		}
	})
}

// printOrder prints an order's receipt and, if the kitchen prints, its
// kitchen ticket.
func (a *app) printOrder(o Order, reprint bool) {
	r := receiptFor(a.withArabic(o), a.cfg)
	r.Copy = reprint
	doc := r.Doc(a.cfg.ReceiptWidth)
	if !a.cfg.PlainReceipts {
		doc.Image = r.Image(paperDots(a.cfg.ReceiptWidth))
	}
	doc.Kick = !reprint && o.Method == payCash && a.cfg.OpenDrawer
	a.printStatus = "Printing the receipt."
	a.enqueuePrint(roleReceipt, "Receipt "+o.Code, doc, func(err error) {
		if err == nil {
			a.printStatus = "Receipt printed."
		} else {
			a.printStatus = ""
		}
	})
	if a.cfg.KitchenEnabled && !reprint {
		if k, ok := kitchenDoc(o, a.cfg.ReceiptWidth); ok {
			a.enqueuePrint(roleKitchen, "Kitchen ticket "+o.Code, k, nil)
		}
	}
}

// enqueuePrint prints a document in the background. Kitchen tickets are
// tried again a few times, as the kitchen can wait for them; a receipt is
// tried once, as its customer can't. A print that fails shows as a note
// with Retry. done, if set, runs with the outcome.
func (a *app) enqueuePrint(role, label string, doc PrintDoc, done func(error)) {
	cfg, send := a.cfg, a.print
	var delays []time.Duration
	if role == roleKitchen {
		delays = a.retryDelays
	}
	a.async(func() func() {
		err := send(cfg, role, doc)
		for i := 0; err != nil && i < len(delays); i++ {
			time.Sleep(delays[i])
			err = send(cfg, role, doc)
		}
		return func() {
			if err != nil {
				a.noteSeq++
				a.printNotes = append(a.printNotes, printNote{id: a.noteSeq, role: role, label: label,
					doc: doc, err: err.Error()})
			}
			if done != nil {
				done(err)
			}
		}
	})
}

func (a *app) retryNote(id int) {
	for i, n := range a.printNotes {
		if n.id == id {
			a.printNotes = append(a.printNotes[:i], a.printNotes[i+1:]...)
			a.enqueuePrint(n.role, n.label, n.doc, nil)
			return
		}
	}
}

func (a *app) dismissNote(id int) {
	for i, n := range a.printNotes {
		if n.id == id {
			a.printNotes = append(a.printNotes[:i], a.printNotes[i+1:]...)
			return
		}
	}
}

// Backups.

// autoBackup backs up if the last backup is older than backupEvery.
func (a *app) autoBackup() {
	store := a.store
	if store == nil {
		return
	}
	a.async(func() func() {
		last, _ := store.LastBackup(context.Background())
		return func() {
			if time.Since(last) >= backupEvery {
				a.backupNow()
			}
		}
	})
}

func (a *app) backupNow() {
	store, dir := a.store, a.cfg.backupDir()
	if store == nil {
		a.settings.backup = errResult("Not backed up: ", a.storeError())
		return
	}
	a.settings.backup = result{text: "Backing up.", busy: true}
	a.async(func() func() {
		path, err := store.Backup(context.Background(), dir, time.Now())
		return func() {
			if err != nil {
				a.settings.backup = errResult("Not backed up: ", err)
				return
			}
			a.settings.backup = result{text: "Backed up to " + path + "."}
		}
	})
}
