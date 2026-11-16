package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/plugins/sqlite"
)

// Order is a completed sale.
type Order struct {
	ID         string // UUIDv7, made on the terminal
	OrgID      string
	ShopID     string
	TerminalID string
	DayID      string
	DayDate    string
	Number     int    // within the business day
	Code       string // as printed: terminal code and number, e.g. "T1-012"
	CreatedAt  time.Time
	StaffID    string
	StaffName  string
	Lines      []Line
	Total      money.Fils
	Method     string // payCash or payCard
	Tendered   money.Fils
	Change     money.Fils
	Status     string // statusCompleted or statusVoided
	VoidedAt   time.Time
	VoidedBy   string // name
	VoidReason string
	Refunds    []Refund
}

const (
	payCash = "cash"
	payCard = "card"

	statusCompleted = "completed"
	statusVoided    = "voided"
)

// Refund is money given back for some of an order's items.
type Refund struct {
	ID         string
	OrderID    string
	CreatedAt  time.Time
	StaffName  string
	ApprovedBy string // name of the manager who approved it
	Reason     string
	Method     string // the order's method: cash back, or back to the card
	Total      money.Fils
	Lines      []RefundLine
}

// RefundLine is how many of an order line were refunded.
type RefundLine struct {
	LineID string
	Name   string
	Qty    int
	Amount money.Fils
}

// Refunded is what the order's refunds gave back.
func (o Order) Refunded() money.Fils {
	var t money.Fils
	for _, r := range o.Refunds {
		t += r.Total
	}
	return t
}

// RefundedQty is how many of a line were refunded.
func (o Order) RefundedQty(lineID string) int {
	n := 0
	for _, r := range o.Refunds {
		for _, l := range r.Lines {
			if l.LineID == lineID {
				n += l.Qty
			}
		}
	}
	return n
}

// Refundable reports whether anything of the order is left to refund.
func (o Order) Refundable() bool {
	if o.Status != statusCompleted {
		return false
	}
	for _, l := range o.Lines {
		if o.RefundedQty(l.ID) < l.Qty {
			return true
		}
	}
	return false
}

// orderCode formats an order number as printed, e.g. "T1-012".
func orderCode(terminal string, n int) string {
	return fmt.Sprintf("%s-%03d", terminal, n)
}

// NextOrderNumber is the number the day's next order gets.
func (s *Store) NextOrderNumber(ctx context.Context, dayID string) (int, error) {
	rows, err := s.query(ctx, "SELECT COALESCE(MAX(number), 0) + 1 FROM orders WHERE business_day_id = ?", dayID)
	if err != nil {
		return 0, err
	}
	return int(firstInt(rows)), nil
}

// SaveOrder writes the order, its lines and modifiers, and its outbox row
// in one transaction: all of them or none. It gives the lines their IDs.
func (s *Store) SaveOrder(ctx context.Context, o *Order) error {
	stmts := []sqlite.Statement{stmt(`INSERT INTO orders (id, org_id, shop_id, terminal_id, business_day_id,
		number, code, created_at, staff_id, subtotal_fils, tax_fils, total_fils, payment_method,
		tendered_fils, change_fils, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		o.ID, o.OrgID, o.ShopID, o.TerminalID, o.DayID, o.Number, o.Code, ts(o.CreatedAt), o.StaffID,
		int64(o.Total), int64(o.Total), o.Method, int64(o.Tendered), int64(o.Change), statusCompleted)}
	for i := range o.Lines {
		l := &o.Lines[i]
		l.ID = newID()
		stmts = append(stmts, stmt(`INSERT INTO order_lines (id, order_id, org_id, shop_id, position, item_id,
			name, qty, unit_price_fils, line_total_fils, kitchen) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID, o.ID, o.OrgID, o.ShopID, i, l.Item.ID, l.Item.Name, l.Qty,
			int64(l.UnitPrice()), int64(l.Total()), b2i(l.Item.Kitchen)))
		for j, m := range l.Mods {
			stmts = append(stmts, stmt(`INSERT INTO order_line_modifiers (id, line_id, order_id, position,
				group_id, group_name, modifier_id, name, price_delta_fils, is_default)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				newID(), l.ID, o.ID, j, m.GroupID, m.GroupName, m.ID, m.Name, int64(m.Delta), b2i(m.Default)))
		}
	}
	stmts = append(stmts, outboxStmt("order", o.ID, o.CreatedAt))
	_, err := s.tx(ctx, stmts)
	return err
}

// OrderSummary is a row of the Orders screen.
type OrderSummary struct {
	ID        string
	Code      string
	DayID     string
	DayDate   string
	CreatedAt time.Time
	StaffName string
	Method    string
	Total     money.Fils
	Status    string
	Refunded  money.Fils
}

// RecentOrders lists the latest orders, newest first.
func (s *Store) RecentOrders(ctx context.Context, limit int) ([]OrderSummary, error) {
	rows, err := s.query(ctx, `SELECT o.id, o.code, o.business_day_id, d.date, o.created_at, COALESCE(st.name, ''),
		o.payment_method, o.total_fils, o.status,
		COALESCE((SELECT SUM(r.total_fils) FROM refunds r WHERE r.order_id = o.id), 0)
		FROM orders o JOIN business_days d ON d.id = o.business_day_id LEFT JOIN staff st ON st.id = o.staff_id
		ORDER BY o.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	out := make([]OrderSummary, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, OrderSummary{ID: str(r[0]), Code: str(r[1]), DayID: str(r[2]), DayDate: str(r[3]),
			CreatedAt: parseTS(r[4]), StaffName: str(r[5]), Method: str(r[6]), Total: money.Fils(i64(r[7])),
			Status: str(r[8]), Refunded: money.Fils(i64(r[9]))})
	}
	return out, nil
}

// Order reads an order with its lines, modifiers and refunds.
func (s *Store) Order(ctx context.Context, id string) (Order, error) {
	rows, err := s.query(ctx, `SELECT o.id, o.org_id, o.shop_id, o.terminal_id, o.business_day_id, d.date,
		o.number, o.code, o.created_at, o.staff_id, COALESCE(st.name, ''), o.total_fils, o.payment_method,
		o.tendered_fils, o.change_fils, o.status, o.voided_at, COALESCE(v.name, ''), COALESCE(o.void_reason, '')
		FROM orders o JOIN business_days d ON d.id = o.business_day_id
		LEFT JOIN staff st ON st.id = o.staff_id LEFT JOIN staff v ON v.id = o.voided_by
		WHERE o.id = ?`, id)
	if err != nil {
		return Order{}, err
	}
	if len(rows.Values) == 0 {
		return Order{}, errNotFound
	}
	r := rows.Values[0]
	o := Order{ID: str(r[0]), OrgID: str(r[1]), ShopID: str(r[2]), TerminalID: str(r[3]), DayID: str(r[4]),
		DayDate: str(r[5]), Number: int(i64(r[6])), Code: str(r[7]), CreatedAt: parseTS(r[8]),
		StaffID: str(r[9]), StaffName: str(r[10]), Total: money.Fils(i64(r[11])), Method: str(r[12]),
		Tendered: money.Fils(i64(r[13])), Change: money.Fils(i64(r[14])), Status: str(r[15]),
		VoidedBy: str(r[17]), VoidReason: str(r[18])}
	if r[16] != nil {
		o.VoidedAt = parseTS(r[16])
	}

	mods, err := s.query(ctx, `SELECT line_id, group_id, group_name, modifier_id, name, price_delta_fils, is_default
		FROM order_line_modifiers WHERE order_id = ? ORDER BY position`, id)
	if err != nil {
		return o, err
	}
	byLine := map[string][]LineMod{}
	for _, m := range mods.Values {
		byLine[str(m[0])] = append(byLine[str(m[0])], LineMod{GroupID: str(m[1]), GroupName: str(m[2]),
			ID: str(m[3]), Name: str(m[4]), Delta: money.Fils(i64(m[5])), Default: i64(m[6]) != 0})
	}
	lines, err := s.query(ctx, `SELECT id, item_id, name, qty, unit_price_fils, kitchen FROM order_lines
		WHERE order_id = ? ORDER BY position`, id)
	if err != nil {
		return o, err
	}
	for _, l := range lines.Values {
		line := Line{ID: str(l[0]), Qty: int(i64(l[3])), Mods: byLine[str(l[0])],
			Item: Item{ID: str(l[1]), Name: str(l[2]), Kitchen: i64(l[5]) != 0}}
		// The unit price was saved with the modifiers in it.
		line.Item.Price = money.Fils(i64(l[4]))
		for _, m := range line.Mods {
			line.Item.Price -= m.Delta
		}
		o.Lines = append(o.Lines, line)
	}

	refunds, err := s.query(ctx, `SELECT r.id, r.created_at, COALESCE(st.name, ''), COALESCE(a.name, ''),
		r.reason, r.method, r.total_fils FROM refunds r
		LEFT JOIN staff st ON st.id = r.staff_id LEFT JOIN staff a ON a.id = r.approved_by
		WHERE r.order_id = ? ORDER BY r.created_at`, id)
	if err != nil {
		return o, err
	}
	names := map[string]string{}
	for _, l := range o.Lines {
		names[l.ID] = l.Item.Name
	}
	for _, rf := range refunds.Values {
		ref := Refund{ID: str(rf[0]), OrderID: id, CreatedAt: parseTS(rf[1]), StaffName: str(rf[2]),
			ApprovedBy: str(rf[3]), Reason: str(rf[4]), Method: str(rf[5]), Total: money.Fils(i64(rf[6]))}
		rls, err := s.query(ctx, "SELECT order_line_id, qty, amount_fils FROM refund_lines WHERE refund_id = ?", ref.ID)
		if err != nil {
			return o, err
		}
		for _, rl := range rls.Values {
			ref.Lines = append(ref.Lines, RefundLine{LineID: str(rl[0]), Name: names[str(rl[0])],
				Qty: int(i64(rl[1])), Amount: money.Fils(i64(rl[2]))})
		}
		o.Refunds = append(o.Refunds, ref)
	}
	return o, nil
}

// VoidOrder cancels an order of the open day that has no refunds.
func (s *Store) VoidOrder(ctx context.Context, orderID, dayID string, by Staff, reason string, now time.Time) error {
	res, err := s.tx(ctx, []sqlite.Statement{
		stmt(`UPDATE orders SET status = ?, voided_at = ?, voided_by = ?, void_reason = ?
			WHERE id = ? AND status = ? AND business_day_id = ?
			AND EXISTS (SELECT 1 FROM business_days WHERE id = ? AND closed_at IS NULL)
			AND NOT EXISTS (SELECT 1 FROM refunds WHERE order_id = ?)`,
			statusVoided, ts(now), by.ID, reason, orderID, statusCompleted, dayID, dayID, orderID),
		outboxIfChanged("order_void", orderID, now),
	})
	if err != nil {
		return err
	}
	if res[0].Changes != 1 {
		return errors.New("only an order of the open day, with no refunds, can be voided")
	}
	return nil
}

// SaveRefund records a refund of an order. qty maps line IDs to how many
// of each to refund.
func (s *Store) SaveRefund(ctx context.Context, cfg Config, o Order, dayID string, qty map[string]int,
	reason string, staff, approver Staff, now time.Time) (Refund, error) {
	if o.Status != statusCompleted {
		return Refund{}, errors.New("a voided order can't be refunded")
	}
	ref := Refund{ID: newID(), OrderID: o.ID, CreatedAt: now, StaffName: staff.Name,
		ApprovedBy: approver.Name, Reason: reason, Method: o.Method}
	var stmts []sqlite.Statement
	for _, l := range o.Lines {
		n := qty[l.ID]
		if n <= 0 {
			continue
		}
		if n > l.Qty-o.RefundedQty(l.ID) {
			return Refund{}, fmt.Errorf("only %d of %s are left to refund", l.Qty-o.RefundedQty(l.ID), l.Item.Name)
		}
		amount := l.UnitPrice().Mul(n)
		ref.Total += amount
		ref.Lines = append(ref.Lines, RefundLine{LineID: l.ID, Name: l.Item.Name, Qty: n, Amount: amount})
		stmts = append(stmts, stmt(`INSERT INTO refund_lines (id, refund_id, order_line_id, qty, amount_fils)
			VALUES (?, ?, ?, ?, ?)`, newID(), ref.ID, l.ID, n, int64(amount)))
	}
	if len(ref.Lines) == 0 {
		return Refund{}, errors.New("choose what to refund")
	}
	stmts = append([]sqlite.Statement{stmt(`INSERT INTO refunds (id, order_id, org_id, shop_id, terminal_id,
		business_day_id, created_at, staff_id, approved_by, reason, method, total_fils)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ref.ID, o.ID, cfg.OrgID, cfg.ShopID, cfg.TerminalID, dayID, ts(now), staff.ID, approver.ID,
		reason, ref.Method, int64(ref.Total))}, stmts...)
	stmts = append(stmts, outboxStmt("refund", ref.ID, now))
	_, err := s.tx(ctx, stmts)
	return ref, err
}
