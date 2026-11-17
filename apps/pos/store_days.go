package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/plugins/sqlite"
)

// BusinessDay runs from its first sale to "Close the day", across midnight
// if the shop is open late, or to the till's end of day, when it closes by
// itself. Orders are numbered within it.
//
// The drawer carries over: a day starts with what the day before left in
// it, counted or not. Only a till's first day starts with a count.
type BusinessDay struct {
	ID           string
	Date         string // the local date it started on, YYYY-MM-DD
	OpenedAt     time.Time
	OpenedBy     string // name
	OpeningFloat money.Fils
	ClosedAt     time.Time // zero while open
	ClosedBy     string    // empty when it closed by itself
	Counted      bool      // the drawer was counted at the close
	CountedCash  money.Fils
}

// Open reports whether the day is still open.
func (d BusinessDay) Open() bool { return d.ClosedAt.IsZero() }

// Auto reports whether the day closed by itself, at the end of day.
func (d BusinessDay) Auto() bool { return !d.Open() && d.ClosedBy == "" }

// DayReport sums a business day: an X report while it is open, the Z
// report once it is closed.
type DayReport struct {
	Day         BusinessDay
	GeneratedAt time.Time
	Orders      int
	Gross       money.Fils // completed orders
	Cash        money.Fils // completed orders paid in cash
	Card        money.Fils
	CashOrders  int
	CardOrders  int
	VoidCount   int
	VoidTotal   money.Fils
	RefundCount int
	RefundCash  money.Fils
	RefundCard  money.Fils
	Movements   []CashMovement
	Items       []ItemSold
}

// ItemSold is one item's sales in a day, before refunds.
type ItemSold struct {
	Name  string
	Qty   int
	Total money.Fils
}

// CashMovement is cash put into the drawer (above 0) or taken out of it
// (below 0), other than by a sale or a refund.
type CashMovement struct {
	ID         string
	At         time.Time
	Amount     money.Fils
	Reason     string
	StaffName  string
	ApprovedBy string // name
}

// Refunds is the day's refunds, by any method.
func (r DayReport) Refunds() money.Fils { return r.RefundCash + r.RefundCard }

// Net is what the day took: sales less refunds.
func (r DayReport) Net() money.Fils { return r.Gross - r.Refunds() }

// Moved is the cash put in, less the cash taken out.
func (r DayReport) Moved() money.Fils {
	var sum money.Fils
	for _, m := range r.Movements {
		sum += m.Amount
	}
	return sum
}

// ExpectedCash is what the drawer should hold: what it started with, plus
// cash sales, less cash refunds, plus cash put in less taken out.
func (r DayReport) ExpectedCash() money.Fils {
	return r.Day.OpeningFloat + r.Cash - r.RefundCash + r.Moved()
}

// Difference is the counted cash less the expected: negative is short. As
// the drawer carries over, it covers every day since the last count.
func (r DayReport) Difference() money.Fils { return r.Day.CountedCash - r.ExpectedCash() }

const daySelect = `SELECT d.id, d.date, d.opened_at, o.name, d.opening_float_fils, d.closed_at,
	COALESCE(c.name, ''), COALESCE(d.counted_cash_fils, 0), d.counted_cash_fils IS NOT NULL
	FROM business_days d JOIN staff o ON o.id = d.opened_by LEFT JOIN staff c ON c.id = d.closed_by`

func scanDay(r []any) BusinessDay {
	d := BusinessDay{ID: str(r[0]), Date: str(r[1]), OpenedAt: parseTS(r[2]), OpenedBy: str(r[3]),
		OpeningFloat: money.Fils(i64(r[4])), ClosedBy: str(r[6]), CountedCash: money.Fils(i64(r[7])),
		Counted: i64(r[8]) != 0}
	if r[5] != nil {
		d.ClosedAt = parseTS(r[5])
	}
	return d
}

// OpenDay returns the day that is open, if one is.
func (s *Store) OpenDay(ctx context.Context) (BusinessDay, bool, error) {
	rows, err := s.query(ctx, daySelect+" WHERE d.closed_at IS NULL ORDER BY d.opened_at DESC LIMIT 1")
	if err != nil || len(rows.Values) == 0 {
		return BusinessDay{}, false, err
	}
	return scanDay(rows.Values[0]), true, nil
}

// Day returns a business day by ID.
func (s *Store) Day(ctx context.Context, id string) (BusinessDay, error) {
	rows, err := s.query(ctx, daySelect+" WHERE d.id = ?", id)
	if err != nil {
		return BusinessDay{}, err
	}
	if len(rows.Values) == 0 {
		return BusinessDay{}, errNotFound
	}
	return scanDay(rows.Values[0]), nil
}

// StartDay opens a business day with the cash in the drawer.
func (s *Store) StartDay(ctx context.Context, cfg Config, by Staff, float money.Fils, now time.Time) (BusinessDay, error) {
	if _, open, err := s.OpenDay(ctx); err != nil {
		return BusinessDay{}, err
	} else if open {
		return BusinessDay{}, errors.New("a day is already open")
	}
	d := BusinessDay{ID: newID(), Date: now.Format("2006-01-02"), OpenedAt: now, OpenedBy: by.Name, OpeningFloat: float}
	_, err := s.tx(ctx, []sqlite.Statement{
		stmt(`INSERT INTO business_days (id, org_id, shop_id, terminal_id, date, opened_at, opened_by,
			opening_float_fils) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, cfg.OrgID, cfg.ShopID, cfg.TerminalID, d.Date, ts(now), by.ID, int64(float)),
		outboxStmt("business_day", d.ID, now),
	})
	return d, err
}

// errFirstDay is EnsureDay's answer on a till that has never had a day: its
// first starts with a count of the drawer.
var errFirstDay = errors.New("count the cash in the drawer to start the till's first day")

// CarryOver is what the drawer should hold now, with no day open: what the
// last day left in it, counted or not. ok is false on a till that has never
// had a day.
func (s *Store) CarryOver(ctx context.Context) (money.Fils, bool, error) {
	rows, err := s.query(ctx, "SELECT id FROM business_days ORDER BY opened_at DESC LIMIT 1")
	if err != nil || len(rows.Values) == 0 {
		return 0, false, err
	}
	r, err := s.Report(ctx, str(rows.Values[0][0]))
	if err != nil {
		return 0, false, err
	}
	if r.Day.Counted {
		return r.Day.CountedCash, true, nil
	}
	return r.ExpectedCash(), true, nil
}

// EnsureDay returns the open day, first opening one if none is, with what
// the drawer carries over. A sale, a refund or cash put in or taken out
// starts a day this way. started reports whether it did.
func (s *Store) EnsureDay(ctx context.Context, cfg Config, by Staff, now time.Time) (day BusinessDay, started bool, err error) {
	day, open, err := s.OpenDay(ctx)
	if err != nil || open {
		return day, false, err
	}
	carry, ok, err := s.CarryOver(ctx)
	if err != nil {
		return day, false, err
	}
	if !ok {
		return day, false, errFirstDay
	}
	day, err = s.StartDay(ctx, cfg, by, carry, now)
	return day, err == nil, err
}

// Report sums a business day.
func (s *Store) Report(ctx context.Context, dayID string) (DayReport, error) {
	day, err := s.Day(ctx, dayID)
	if err != nil {
		return DayReport{}, err
	}
	r := DayReport{Day: day, GeneratedAt: time.Now()}
	rows, err := s.query(ctx, `SELECT payment_method, COUNT(*), COALESCE(SUM(total_fils), 0) FROM orders
		WHERE business_day_id = ? AND status = 'completed' GROUP BY payment_method`, dayID)
	if err != nil {
		return r, err
	}
	for _, row := range rows.Values {
		n, total := int(i64(row[1])), money.Fils(i64(row[2]))
		r.Orders += n
		r.Gross += total
		if str(row[0]) == payCash {
			r.Cash, r.CashOrders = total, n
		} else {
			r.Card, r.CardOrders = r.Card+total, r.CardOrders+n
		}
	}
	rows, err = s.query(ctx, `SELECT COUNT(*), COALESCE(SUM(total_fils), 0) FROM orders
		WHERE business_day_id = ? AND status = 'voided'`, dayID)
	if err != nil {
		return r, err
	}
	r.VoidCount, r.VoidTotal = int(i64(rows.Values[0][0])), money.Fils(i64(rows.Values[0][1]))
	rows, err = s.query(ctx, `SELECT method, COUNT(*), COALESCE(SUM(total_fils), 0) FROM refunds
		WHERE business_day_id = ? GROUP BY method`, dayID)
	if err != nil {
		return r, err
	}
	for _, row := range rows.Values {
		r.RefundCount += int(i64(row[1]))
		if str(row[0]) == payCash {
			r.RefundCash += money.Fils(i64(row[2]))
		} else {
			r.RefundCard += money.Fils(i64(row[2]))
		}
	}
	rows, err = s.query(ctx, `SELECT m.id, m.created_at, m.amount_fils, m.reason, COALESCE(st.name, ''), COALESCE(a.name, '')
		FROM cash_movements m LEFT JOIN staff st ON st.id = m.staff_id LEFT JOIN staff a ON a.id = m.approved_by
		WHERE m.business_day_id = ? ORDER BY m.created_at`, dayID)
	if err != nil {
		return r, err
	}
	for _, row := range rows.Values {
		r.Movements = append(r.Movements, CashMovement{ID: str(row[0]), At: parseTS(row[1]),
			Amount: money.Fils(i64(row[2])), Reason: str(row[3]), StaffName: str(row[4]), ApprovedBy: str(row[5])})
	}
	rows, err = s.query(ctx, `SELECT l.name, SUM(l.qty), SUM(l.line_total_fils) FROM order_lines l
		JOIN orders o ON o.id = l.order_id
		WHERE o.business_day_id = ? AND o.status = 'completed'
		GROUP BY l.item_id, l.name ORDER BY SUM(l.qty) DESC, l.name`, dayID)
	if err != nil {
		return r, err
	}
	for _, row := range rows.Values {
		r.Items = append(r.Items, ItemSold{Name: str(row[0]), Qty: int(i64(row[1])), Total: money.Fils(i64(row[2]))})
	}
	return r, nil
}

// CloseDay closes the open day, and keeps its report with it. by is the
// manager who closed it, or nil when it closes by itself at the end of
// day. counted is the cash counted in the drawer, or nil when it wasn't.
func (s *Store) CloseDay(ctx context.Context, dayID string, by *Staff, counted *money.Fils, now time.Time) (DayReport, error) {
	r, err := s.Report(ctx, dayID)
	if err != nil {
		return r, err
	}
	if !r.Day.Open() {
		return r, errors.New("the day is already closed")
	}
	var byID, countedArg any
	r.Day.ClosedAt = now
	if by != nil {
		r.Day.ClosedBy, byID = by.Name, by.ID
	}
	if counted != nil {
		r.Day.Counted, r.Day.CountedCash, countedArg = true, *counted, int64(*counted)
	}
	r.GeneratedAt = now
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	res, err := s.tx(ctx, []sqlite.Statement{
		stmt(`UPDATE business_days SET closed_at = ?, closed_by = ?, counted_cash_fils = ?, report = ?
			WHERE id = ? AND closed_at IS NULL`, ts(now), byID, countedArg, string(data), dayID),
		outboxIfChanged("business_day", dayID, now),
	})
	if err != nil {
		return r, err
	}
	if res[0].Changes != 1 {
		return r, errors.New("the day was closed meanwhile")
	}
	return r, nil
}

// AddCashMovement records cash put into the open day's drawer (amount above
// 0) or taken out of it (below 0), by someone, approved by a manager.
func (s *Store) AddCashMovement(ctx context.Context, cfg Config, dayID string, by, approver Staff, amount money.Fils,
	reason string, now time.Time) (CashMovement, error) {
	m := CashMovement{ID: newID(), At: now, Amount: amount, Reason: reason, StaffName: by.Name, ApprovedBy: approver.Name}
	if amount == 0 {
		return m, errors.New("enter an amount")
	}
	res, err := s.tx(ctx, []sqlite.Statement{
		stmt(`INSERT INTO cash_movements (id, org_id, shop_id, terminal_id, business_day_id, created_at, staff_id,
			approved_by, amount_fils, reason)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM business_days WHERE id = ? AND closed_at IS NULL)`,
			m.ID, cfg.OrgID, cfg.ShopID, cfg.TerminalID, dayID, ts(now), by.ID, approver.ID, int64(amount), reason, dayID),
		outboxIfChanged("cash_movement", m.ID, now),
	})
	if err != nil {
		return m, err
	}
	if res[0].Changes != 1 {
		return m, errors.New("the day closed meanwhile")
	}
	return m, nil
}

// dayEndAfter is when a day opened at opened closes by itself: the first
// time after it that the clock shows ends, such as "04:00". ok is false
// when ends isn't a time, as when days don't close by themselves.
func dayEndAfter(opened time.Time, ends string) (end time.Time, ok bool) {
	t, err := time.Parse("15:04", ends)
	if err != nil {
		return time.Time{}, false
	}
	o := opened.Local()
	end = time.Date(o.Year(), o.Month(), o.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	if !end.After(o) {
		end = end.AddDate(0, 0, 1)
	}
	return end, true
}

// CloseIfOver closes the open day by itself, as at its end of day, if that
// has passed. It reports the day it closed, if it did.
func (s *Store) CloseIfOver(ctx context.Context, ends string, now time.Time) (*DayReport, error) {
	day, open, err := s.OpenDay(ctx)
	if err != nil || !open {
		return nil, err
	}
	end, ok := dayEndAfter(day.OpenedAt, ends)
	if !ok || now.Before(end) {
		return nil, nil
	}
	r, err := s.CloseDay(ctx, day.ID, nil, nil, end)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
