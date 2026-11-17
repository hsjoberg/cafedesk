package main

import (
	"context"
	"fmt"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo"
)

// Orders screen.

// refundState is the refund being put together for the selected order.
type refundState struct {
	qty    map[string]int // by line ID
	reason string
	err    string
	saving bool
}

func (r *refundState) total(o Order) money.Fils {
	var t money.Fils
	for _, l := range o.Lines {
		t += l.UnitPrice().Mul(r.qty[l.ID])
	}
	return t
}

// voidState is the void being confirmed for the selected order.
type voidState struct {
	reason string
	err    string
}

// reasonTestOrder voids an order rung up to try the till or its printer:
// it isn't a sale, so neither the day report nor the admin counts it, even
// as a void. The admin knows it by this text.
const reasonTestOrder = "Test order"

// reasons offered for voids and refunds.
var reasons = []string{
	"Wrong item rung up",
	"Customer changed their mind",
	"Problem with the food or drink",
	reasonTestOrder,
	"Other",
}

func (a *app) openOrders() {
	a.screen, a.sheet = screenOrders, sheetNone
	a.order, a.refund, a.voiding = nil, nil, nil
	a.loadOrders("")
}

// loadOrders reads the latest orders, then selects selectID, or the newest.
func (a *app) loadOrders(selectID string) {
	store := a.store
	if store == nil {
		a.ordersErr = a.storeError().Error()
		return
	}
	a.async(func() func() {
		list, err := store.RecentOrders(context.Background(), 200)
		return func() {
			if err != nil {
				a.ordersErr = "Couldn't read the orders: " + err.Error()
				return
			}
			a.orders, a.ordersErr = list, ""
			if selectID == "" && len(list) > 0 {
				selectID = list[0].ID
			}
			if selectID != "" {
				a.selectOrder(selectID)
			}
		}
	})
}

func (a *app) selectOrder(id string) {
	store := a.store
	a.refund, a.voiding = nil, nil
	a.async(func() func() {
		o, err := store.Order(context.Background(), id)
		return func() {
			if err != nil {
				a.ordersErr = "Couldn't read the order: " + err.Error()
				return
			}
			a.order = &o
		}
	})
}

func (a *app) reprintOrder() {
	if a.order != nil {
		a.printOrder(*a.order, true)
	}
}

func (a *app) startRefund() {
	if a.order == nil || !a.order.Refundable() {
		return
	}
	a.voiding = nil
	a.refund = &refundState{qty: map[string]int{}}
}

func (a *app) refundStep(lineID string, delta int) {
	r, o := a.refund, a.order
	if r == nil || o == nil {
		return
	}
	for _, l := range o.Lines {
		if l.ID == lineID {
			left := l.Qty - o.RefundedQty(l.ID)
			r.qty[lineID] = min(max(r.qty[lineID]+delta, 0), left)
		}
	}
}

func (a *app) confirmRefund() {
	r, o := a.refund, a.order
	if r == nil || o == nil || a.firstDay || a.staff == nil || r.saving {
		return
	}
	if r.reason == "" {
		r.err = "Choose a reason."
		return
	}
	total := r.total(*o)
	if total == 0 {
		r.err = "Choose what to refund."
		return
	}
	a.withManager(fmt.Sprintf("Refund %s on %s", total.WithCode(), o.Code), func(mgr Staff) {
		store, cfg, staff, order := a.store, a.cfg, *a.staff, *o
		qty, reason := r.qty, r.reason
		r.saving = true
		a.async(func() func() {
			ctx, now := context.Background(), time.Now()
			// A refund belongs to the open day; with none, it starts one.
			day, _, err := store.EnsureDay(ctx, cfg, staff, now)
			var ref Refund
			if err == nil {
				ref, err = store.SaveRefund(ctx, cfg, order, day.ID, qty, reason, staff, mgr, now)
			}
			return func() {
				r.saving = false
				if err != nil {
					r.err = "Not refunded: " + err.Error()
					return
				}
				a.day = &day
				a.refund = nil
				doc := refundDoc(order, ref, a.cfg)
				if !a.cfg.PlainReceipts {
					doc.Image = refundImage(a.withArabic(order), ref, a.cfg, paperDots(a.cfg.ReceiptWidth))
				}
				doc.Kick = ref.Method == payCash && a.cfg.OpenDrawer
				a.enqueuePrint(roleReceipt, "Refund slip "+order.Code, doc, nil)
				a.loadOrders(order.ID)
				a.syncNow()
			}
		})
	})
}

func (a *app) startVoid() {
	if a.order == nil || a.order.Status != statusCompleted || len(a.order.Refunds) > 0 {
		return
	}
	a.refund = nil
	a.voiding = &voidState{}
}

func (a *app) confirmVoid() {
	v, o := a.voiding, a.order
	if v == nil || o == nil || a.day == nil {
		return
	}
	if v.reason == "" {
		v.err = "Choose a reason."
		return
	}
	a.withManager("Void "+o.Code, func(mgr Staff) {
		store, day, id, reason := a.store, a.day.ID, o.ID, v.reason
		a.async(func() func() {
			err := store.VoidOrder(context.Background(), id, day, mgr, reason, time.Now())
			return func() {
				if err != nil {
					v.err = "Not voided: " + err.Error()
					return
				}
				a.voiding = nil
				a.loadOrders(id)
				a.syncNow()
			}
		})
	})
}

// canVoid reports whether the selected order may be voided: it belongs to
// the open day, isn't voided, and has no refunds.
func (a *app) canVoid() bool {
	o := a.order
	return o != nil && a.day != nil && o.DayID == a.day.ID && o.Status == statusCompleted && len(o.Refunds) == 0
}

// Day screen.

func (a *app) openDayReport() {
	a.screen, a.sheet = screenDay, sheetNone
	a.counted = amountEntry{}
	a.dayErr = ""
	a.loadReport()
}

func (a *app) loadReport() {
	if a.day == nil || a.store == nil {
		a.report = nil
		return
	}
	store, id := a.store, a.day.ID
	a.async(func() func() {
		r, err := store.Report(context.Background(), id)
		return func() {
			if err != nil {
				a.dayErr = "Couldn't read the day: " + err.Error()
				return
			}
			a.report = &r
		}
	})
}

func (a *app) printReport() {
	if a.report != nil {
		a.enqueuePrint(roleReceipt, "Day report", reportDoc(*a.report, a.cfg), nil)
	}
}

// closeDay closes the open day, with the cash counted in the drawer if
// one was entered: the count is optional, and covers every day since the
// last one.
func (a *app) closeDay() {
	if a.day == nil || a.store == nil {
		return
	}
	a.withManager("Close the day", func(mgr Staff) {
		store, id := a.store, a.day.ID
		var counted *money.Fils
		if a.counted.text != "" {
			c := a.counted.value()
			counted = &c
		}
		a.async(func() func() {
			r, err := store.CloseDay(context.Background(), id, &mgr, counted, time.Now())
			return func() {
				if err != nil {
					a.dayErr = "The day isn't closed: " + err.Error()
					return
				}
				a.enqueuePrint(roleReceipt, "Day closed", reportDoc(r, a.cfg), nil)
				a.dayClosed(r)
			}
		})
	})
}

// Cash in or out.

// cashMoveState is cash being put into the drawer or taken out of it.
type cashMoveState struct {
	out      bool // taking cash out
	amount   amountEntry
	reason   string
	err      string
	saving   bool
	approver Staff
}

// Reasons for cash in or out, as the buttons offer them.
var (
	cashOutReasons = []string{"Banked", "Paid a supplier", "Taken by the owner", "Other"}
	cashInReasons  = []string{"Change added", "Other"}
)

// openCashMove starts putting cash in or taking it out, with a manager's
// approval.
func (a *app) openCashMove() {
	if a.firstDay || a.staff == nil {
		return
	}
	a.withManager("Cash in or out", func(mgr Staff) {
		a.cashMove = &cashMoveState{out: true, approver: mgr}
	})
}

// saveCashMove records it in the open day, starting one if none is open.
func (a *app) saveCashMove() {
	m := a.cashMove
	if m == nil || m.saving || a.staff == nil || a.store == nil {
		return
	}
	amount := m.amount.value()
	switch {
	case amount <= 0:
		m.err = "Enter the amount."
		return
	case m.reason == "":
		m.err = "Choose a reason."
		return
	}
	if m.out {
		amount = -amount
	}
	store, cfg, by, approver, reason := a.store, a.cfg, *a.staff, m.approver, m.reason
	m.saving, m.err = true, ""
	a.async(func() func() {
		ctx, now := context.Background(), time.Now()
		day, _, err := store.EnsureDay(ctx, cfg, by, now)
		if err == nil {
			_, err = store.AddCashMovement(ctx, cfg, day.ID, by, approver, amount, reason, now)
		}
		return func() {
			m.saving = false
			if err != nil {
				m.err = "Not saved: " + err.Error()
				return
			}
			a.cashMove = nil
			a.day = &day
			a.loadReport()
			a.syncNow()
		}
	})
}

// Settings.

// settingsState is the Settings screen.
type settingsState struct {
	by     *Staff // the manager the screen was opened for
	staff  []Staff
	msg    result
	print  result
	backup result
	last   time.Time
	lastAt string
	adding *Staff // a person being added: name typed, PIN next
}

func (a *app) openSettings() {
	a.withManager("Open Settings", func(mgr Staff) {
		a.settings.by = &mgr
		a.screen, a.sheet = screenSettings, sheetNone
		a.settings.msg, a.settings.print = result{}, result{}
		if !a.checks.printersLoaded {
			a.loadPrinters()
		}
		a.loadSettings()
	})
}

func (a *app) loadSettings() {
	store := a.store
	if store == nil {
		a.settings.msg = errResult("", a.storeError())
		return
	}
	a.async(func() func() {
		ctx := context.Background()
		list, err := store.AllStaff(ctx)
		last, path := store.LastBackup(ctx)
		return func() {
			if err != nil {
				a.settings.msg = errResult("Couldn't read the staff: ", err)
				return
			}
			a.settings.staff, a.settings.last, a.settings.lastAt = list, last, path
		}
	})
}

func (a *app) saveConfig() {
	if err := saveConfig(a.cfgDir, a.cfg); err != nil {
		a.settings.msg = errResult("Settings not saved: ", err)
	}
}

func (a *app) editText(title, value string, done func(string)) {
	a.keyboard = &keyboard{title: title, text: value, done: done}
}

func (a *app) testPrint(role string) {
	a.settings.print = result{text: "Printing a test.", busy: true}
	a.enqueuePrint(role, "Test print", testDoc(a.cfg, role), func(err error) {
		if err != nil {
			a.settings.print = errResult("Test not printed: ", err)
			return
		}
		name, _ := a.cfg.printer(role)
		a.settings.print = result{text: "Sent to " + name + ". Check that it printed and cut."}
	})
}

func (a *app) addStaffName() {
	a.editText("Name of the new person", "", func(name string) {
		if name == "" {
			return
		}
		a.settings.adding = &Staff{Name: name, Role: roleCashier}
		a.pinChange = &pinChange{name: name}
	})
}

// staffPINChosen finishes adding a person once their PIN is typed twice.
func (a *app) staffPINChosen(pin string) {
	st := a.settings.adding
	a.settings.adding = nil
	if st == nil || a.store == nil {
		return
	}
	store := a.store
	a.async(func() func() {
		added, err := store.AddStaff(context.Background(), st.Name, st.Role, pin)
		return func() {
			if err != nil {
				a.settings.msg = errResult(st.Name+" wasn't added: ", err)
				return
			}
			a.settings.msg = result{text: added.Name + " can now sign in with their PIN."}
			a.loadSettings()
		}
	})
}

func (a *app) updateStaff(st Staff) {
	store := a.store
	a.async(func() func() {
		err := store.UpdateStaff(context.Background(), st)
		return func() {
			if err != nil {
				a.settings.msg = errResult("Not changed: ", err)
				return
			}
			a.settings.msg = result{}
			a.loadSettings()
		}
	})
}

func (a *app) chooseBackupFolder() {
	if a.win == nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Choose a folder for backups",
		DefaultPath: a.cfg.backupDir(), Directory: true})
	if err != nil {
		a.settings.backup = errResult("", err)
		return
	}
	if len(paths) == 1 {
		a.cfg.BackupDir = paths[0]
		a.saveConfig()
		a.backupNow()
	}
}

// Checks screen.

func (a *app) loadPrinters() {
	a.checks.printersLoaded = true
	a.async(func() func() {
		names, err := listPrinters()
		def, _ := defaultPrinter()
		return func() {
			a.checks.printers, a.checks.defaultPrinter = names, def
			if err != nil {
				a.settings.print = errResult("Couldn't list the printers: ", err)
			}
		}
	})
}

func (a *app) checkDatabase() {
	store, storeErr := a.store, a.storeError()
	a.checks.db = result{text: "Writing a row.", busy: true}
	a.async(func() func() {
		if store == nil {
			return func() { a.checks.db = errResult("Failed: ", storeErr) }
		}
		ctx := context.Background()
		n, err := store.WriteCheck(ctx)
		st, serr := store.Stats(ctx)
		return func() {
			switch {
			case err != nil:
				a.checks.db = errResult("Failed: ", err)
			case serr != nil:
				a.checks.db = errResult("Failed: ", serr)
			default:
				a.checks.db = result{text: fmt.Sprintf(
					"Wrote and read back row %d. %d orders saved, %d changes waiting to sync. Restart the PC, run this again, and the row count should keep growing.",
					n, st.Orders, st.Unsent)}
			}
		}
	})
}

func (a *app) checkServer() {
	url := a.cfg.ServerURL
	a.checks.srv = result{text: "Calling " + url + ".", busy: true}
	a.async(func() func() {
		p, err := pingServer(context.Background(), url)
		return func() {
			if err != nil {
				a.checks.srv = errResult("Failed: ", err)
				return
			}
			skew := time.Until(p.ServerTime).Round(time.Second)
			if skew < 0 {
				skew = -skew
			}
			a.checks.srv = result{text: fmt.Sprintf("Reached in %d ms. Server version %s. Clocks differ by %s.",
				p.Latency.Milliseconds(), p.Version, skew)}
		}
	})
}

func (a *app) setStartAtLogin(on bool) result {
	if a.setOpenAtLogin == nil {
		return result{text: "Only the running app can change this.", err: true}
	}
	if err := a.setOpenAtLogin(on); err != nil {
		return errResult("Failed: ", err)
	}
	a.openAtLogin = on
	return result{}
}

func (a *app) setFullScreen(on bool) {
	a.cfg.FullScreen = on
	a.saveConfig()
	if a.win != nil {
		a.win.SetFullScreen(on)
	}
}
