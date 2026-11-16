package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/plugins/sqlite"
)

// BusinessDay runs from "Start the day" to "Close the day", across
// midnight if the shop is open late. Orders are numbered within it.
type BusinessDay struct {
	ID           string
	Date         string // the local date it started on, YYYY-MM-DD
	OpenedAt     time.Time
	OpenedBy     string // name
	OpeningFloat money.Fils
	ClosedAt     time.Time // zero while open
	ClosedBy     string
	CountedCash  money.Fils
}

// Open reports whether the day is still open.
func (d BusinessDay) Open() bool { return d.ClosedAt.IsZero() }

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
	Items       []ItemSold
}

// ItemSold is one item's sales in a day, before refunds.
type ItemSold struct {
	Name  string
	Qty   int
	Total money.Fils
}

// Refunds is the day's refunds, by any method.
func (r DayReport) Refunds() money.Fils { return r.RefundCash + r.RefundCard }

// Net is what the day took: sales less refunds.
func (r DayReport) Net() money.Fils { return r.Gross - r.Refunds() }

// ExpectedCash is what the drawer should hold: the float, plus cash sales,
// less cash refunds.
func (r DayReport) ExpectedCash() money.Fils { return r.Day.OpeningFloat + r.Cash - r.RefundCash }

// Difference is the counted cash less the expected: negative is short.
func (r DayReport) Difference() money.Fils { return r.Day.CountedCash - r.ExpectedCash() }

const daySelect = `SELECT d.id, d.date, d.opened_at, o.name, d.opening_float_fils, d.closed_at,
	COALESCE(c.name, ''), COALESCE(d.counted_cash_fils, 0)
	FROM business_days d JOIN staff o ON o.id = d.opened_by LEFT JOIN staff c ON c.id = d.closed_by`

func scanDay(r []any) BusinessDay {
	d := BusinessDay{ID: str(r[0]), Date: str(r[1]), OpenedAt: parseTS(r[2]), OpenedBy: str(r[3]),
		OpeningFloat: money.Fils(i64(r[4])), ClosedBy: str(r[6]), CountedCash: money.Fils(i64(r[7]))}
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

// CloseDay closes the open day with the cash counted in the drawer, and
// keeps its report with it.
func (s *Store) CloseDay(ctx context.Context, dayID string, by Staff, counted money.Fils, now time.Time) (DayReport, error) {
	r, err := s.Report(ctx, dayID)
	if err != nil {
		return r, err
	}
	if !r.Day.Open() {
		return r, errors.New("the day is already closed")
	}
	r.Day.ClosedAt, r.Day.ClosedBy, r.Day.CountedCash = now, by.Name, counted
	r.GeneratedAt = now
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	res, err := s.tx(ctx, []sqlite.Statement{
		stmt(`UPDATE business_days SET closed_at = ?, closed_by = ?, counted_cash_fils = ?, report = ?
			WHERE id = ? AND closed_at IS NULL`, ts(now), by.ID, int64(counted), string(data), dayID),
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
