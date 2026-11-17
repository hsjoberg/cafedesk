package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cafedesk/apps/pos/api"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/google/uuid"
)

// Syncing with the server: changes to the menu and staff come down into the
// tables the till reads, and what happened on the till goes up from the
// outbox.

// SyncVersion is the server's catalog version the till last applied; 0
// before its first sync.
func (s *Store) SyncVersion(ctx context.Context) (int64, error) {
	v, err := s.Meta(ctx, "sync_version")
	if err != nil || v == "" {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// ResetSync makes the next sync fetch everything, as after pairing.
func (s *Store) ResetSync(ctx context.Context) error { return s.SetMeta(ctx, "sync_version", "0") }

// ApplyChanges writes what the server sent, in one transaction. A full set
// replaces the menu, and the staff if it has an active manager: the till
// keeps its own staff rather than lock everyone out.
func (s *Store) ApplyChanges(ctx context.Context, ch api.Changes) error {
	var st []sqlite.Statement
	if ch.Full {
		for _, table := range []string{"categories", "items", "modifier_groups", "modifiers"} {
			st = append(st, stmt("UPDATE "+table+" SET active = 0"))
		}
		if hasActiveManager(ch.Staff) {
			st = append(st, stmt("UPDATE staff SET active = 0"))
		}
	}
	for _, c := range ch.Categories {
		st = append(st, stmt(`INSERT INTO categories (id, name, name_ar, color, position, kitchen, active)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name = excluded.name, name_ar = excluded.name_ar,
			color = excluded.color, position = excluded.position, kitchen = excluded.kitchen, active = excluded.active`,
			c.Id.String(), c.Name, c.NameAr, c.Color, c.Position, b2i(c.Kitchen), b2i(!c.Archived)))
	}
	for _, g := range ch.ModifierGroups {
		st = append(st, stmt(`INSERT INTO modifier_groups (id, name, name_ar, multiple, position, active)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name = excluded.name, name_ar = excluded.name_ar,
			multiple = excluded.multiple, position = excluded.position, active = excluded.active`,
			g.Id.String(), g.Name, g.NameAr, b2i(g.Multiple), g.Position, b2i(!g.Archived)))
		for _, m := range g.Modifiers {
			st = append(st, stmt(`INSERT INTO modifiers (id, group_id, name, name_ar, price_delta_fils, is_default, position, active)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET group_id = excluded.group_id,
				name = excluded.name, name_ar = excluded.name_ar, price_delta_fils = excluded.price_delta_fils,
				is_default = excluded.is_default, position = excluded.position, active = excluded.active`,
				m.Id.String(), g.Id.String(), m.Name, m.NameAr, m.PriceDeltaFils, b2i(m.IsDefault), m.Position,
				b2i(!m.Archived && !g.Archived)))
		}
	}
	for _, it := range ch.Items {
		id := it.Id.String()
		st = append(st, stmt(`INSERT INTO items (id, category_id, name, name_ar, price_fils, position, kitchen, active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET category_id = excluded.category_id,
			name = excluded.name, name_ar = excluded.name_ar, price_fils = excluded.price_fils,
			position = excluded.position, kitchen = excluded.kitchen, active = excluded.active`,
			id, it.CategoryId.String(), it.Name, it.NameAr, it.PriceFils, it.Position, b2i(it.Kitchen), b2i(!it.Archived)),
			stmt("DELETE FROM item_modifier_groups WHERE item_id = ?", id))
		for i, g := range it.ModifierGroupIds {
			st = append(st, stmt("INSERT OR IGNORE INTO item_modifier_groups (item_id, group_id, position) VALUES (?, ?, ?)",
				id, g.String(), i))
		}
	}
	for _, p := range ch.Staff {
		st = append(st, stmt(`INSERT INTO staff (id, name, role, pin_hash, active, must_change_pin, created_at, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'server') ON CONFLICT(id) DO UPDATE SET name = excluded.name, role = excluded.role,
			pin_hash = excluded.pin_hash, active = excluded.active, must_change_pin = excluded.must_change_pin,
			source = 'server'`,
			p.Id.String(), p.Name, string(p.Role), p.PinHash, b2i(p.Active), b2i(p.MustChangePin), ts(time.Now())))
	}
	st = append(st, stmt("INSERT OR REPLACE INTO meta (key, value) VALUES ('sync_version', ?)", strconv.FormatInt(ch.Version, 10)))
	_, err := s.tx(ctx, st)
	return err
}

func hasActiveManager(staff []api.SyncStaff) bool {
	for _, p := range staff {
		if p.Active && p.Role == api.StaffRoleManager {
			return true
		}
	}
	return false
}

// PendingCount is how many events wait to be sent.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	rows, err := s.query(ctx, "SELECT COUNT(*) FROM outbox WHERE sent_at IS NULL")
	return int(firstInt(rows)), err
}

// PendingEvents builds the oldest events not yet sent, at most limit, from
// the rows they point at.
func (s *Store) PendingEvents(ctx context.Context, limit int) ([]api.Event, error) {
	rows, err := s.query(ctx, `SELECT seq, entity, entity_id, COALESCE(payload, '') FROM outbox
		WHERE sent_at IS NULL ORDER BY seq LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	events := make([]api.Event, 0, len(rows.Values))
	for _, r := range rows.Values {
		ev := api.Event{Seq: i64(r[0])}
		id, payload := str(r[2]), str(r[3])
		switch str(r[1]) {
		case "business_day":
			ev.Type = api.EventTypeBusinessDay
			ev.BusinessDay, err = s.dayEvent(ctx, id)
		case "order":
			ev.Type = api.EventTypeOrder
			ev.Order, err = s.orderEvent(ctx, id)
		case "order_void":
			ev.Type = api.EventTypeOrderVoid
			ev.OrderVoid, err = s.voidEvent(ctx, id)
		case "refund":
			ev.Type = api.EventTypeRefund
			ev.Refund, err = s.refundEvent(ctx, id)
		case "cash_movement":
			ev.Type = api.EventTypeCashMovement
			ev.CashMovement, err = s.cashMovementEvent(ctx, id)
		case "staff_pin":
			ev.Type = api.EventTypeStaffPin
			ev.StaffPin = &api.StaffPinEvent{}
			err = json.Unmarshal([]byte(payload), ev.StaffPin)
		default:
			err = fmt.Errorf("unknown kind %q", str(r[1]))
		}
		if err != nil {
			return events, fmt.Errorf("event %d (%s %s): %w", ev.Seq, str(r[1]), id, err)
		}
		events = append(events, ev)
	}
	return events, nil
}

// MarkSent marks events as taken by the server.
func (s *Store) MarkSent(ctx context.Context, seqs []int64, at time.Time) error {
	if len(seqs) == 0 {
		return nil
	}
	marks := make([]string, len(seqs))
	args := []any{ts(at)}
	for i, seq := range seqs {
		marks[i] = "?"
		args = append(args, seq)
	}
	_, err := s.exec(ctx, "UPDATE outbox SET sent_at = ? WHERE seq IN ("+strings.Join(marks, ", ")+")", args...)
	return err
}

// uid reads an ID the till made, a UUID.
func uid(v any) uuid.UUID {
	id, _ := uuid.Parse(str(v))
	return id
}

func (s *Store) dayEvent(ctx context.Context, id string) (*api.BusinessDayEvent, error) {
	rows, err := s.query(ctx, `SELECT d.id, d.date, d.opened_at, d.opened_by, COALESCE(o.name, ''), d.opening_float_fils,
		d.closed_at, d.closed_by, COALESCE(c.name, ''), d.counted_cash_fils
		FROM business_days d LEFT JOIN staff o ON o.id = d.opened_by LEFT JOIN staff c ON c.id = d.closed_by
		WHERE d.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, errNotFound
	}
	r := rows.Values[0]
	d := &api.BusinessDayEvent{Id: uid(r[0]), Date: str(r[1]), OpenedAt: parseTS(r[2]).UTC(), OpenedBy: uid(r[3]),
		OpenedByName: str(r[4]), OpeningFloatFils: i64(r[5])}
	if r[6] != nil {
		closed := parseTS(r[6]).UTC()
		d.ClosedAt = &closed
		if r[7] != nil { // closed by someone, not by itself
			by, name := uid(r[7]), str(r[8])
			d.ClosedBy, d.ClosedByName = &by, &name
		}
		if r[9] != nil { // counted
			counted := i64(r[9])
			d.CountedCashFils = &counted
		}
	}
	return d, nil
}

func (s *Store) orderEvent(ctx context.Context, id string) (*api.OrderEvent, error) {
	o, err := s.Order(ctx, id)
	if err != nil {
		return nil, err
	}
	ev := &api.OrderEvent{Id: uid(o.ID), BusinessDayId: uid(o.DayID), Number: o.Number, Code: o.Code,
		CreatedAt: o.CreatedAt.UTC(), StaffId: uid(o.StaffID), StaffName: o.StaffName, TotalFils: int64(o.Total),
		PaymentMethod: api.PaymentMethod(o.Method), TenderedFils: int64(o.Tendered), ChangeFils: int64(o.Change),
		Lines: make([]api.OrderLineEvent, 0, len(o.Lines))}
	for _, l := range o.Lines {
		le := api.OrderLineEvent{Id: uid(l.ID), ItemId: l.Item.ID, Name: l.Item.Name, Qty: l.Qty,
			UnitPriceFils: int64(l.UnitPrice()), LineTotalFils: int64(l.Total()), Kitchen: l.Item.Kitchen,
			Modifiers: make([]api.OrderLineModifierEvent, 0, len(l.Mods))}
		for _, m := range l.Mods {
			le.Modifiers = append(le.Modifiers, api.OrderLineModifierEvent{GroupId: m.GroupID, GroupName: m.GroupName,
				ModifierId: m.ID, Name: m.Name, PriceDeltaFils: int64(m.Delta), IsDefault: m.Default})
		}
		ev.Lines = append(ev.Lines, le)
	}
	return ev, nil
}

func (s *Store) voidEvent(ctx context.Context, id string) (*api.OrderVoidEvent, error) {
	rows, err := s.query(ctx, `SELECT o.voided_at, o.voided_by, COALESCE(v.name, ''), COALESCE(o.void_reason, '')
		FROM orders o LEFT JOIN staff v ON v.id = o.voided_by WHERE o.id = ? AND o.status = ?`, id, statusVoided)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, errNotFound
	}
	r := rows.Values[0]
	return &api.OrderVoidEvent{OrderId: uid(id), VoidedAt: parseTS(r[0]).UTC(), VoidedBy: uid(r[1]),
		VoidedByName: str(r[2]), Reason: str(r[3])}, nil
}

func (s *Store) refundEvent(ctx context.Context, id string) (*api.RefundEvent, error) {
	rows, err := s.query(ctx, `SELECT r.order_id, r.business_day_id, r.created_at, r.staff_id, COALESCE(st.name, ''),
		r.approved_by, COALESCE(a.name, ''), r.reason, r.method, r.total_fils
		FROM refunds r LEFT JOIN staff st ON st.id = r.staff_id LEFT JOIN staff a ON a.id = r.approved_by WHERE r.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, errNotFound
	}
	r := rows.Values[0]
	ev := &api.RefundEvent{Id: uid(id), OrderId: uid(r[0]), BusinessDayId: uid(r[1]), CreatedAt: parseTS(r[2]).UTC(),
		StaffId: uid(r[3]), StaffName: str(r[4]), ApprovedBy: uid(r[5]), ApprovedByName: str(r[6]), Reason: str(r[7]),
		Method: api.PaymentMethod(str(r[8])), TotalFils: i64(r[9]), Lines: []api.RefundLineEvent{}}
	lines, err := s.query(ctx, "SELECT order_line_id, qty, amount_fils FROM refund_lines WHERE refund_id = ?", id)
	if err != nil {
		return nil, err
	}
	for _, l := range lines.Values {
		ev.Lines = append(ev.Lines, api.RefundLineEvent{OrderLineId: uid(l[0]), Qty: int(i64(l[1])), AmountFils: i64(l[2])})
	}
	return ev, nil
}

func (s *Store) cashMovementEvent(ctx context.Context, id string) (*api.CashMovementEvent, error) {
	rows, err := s.query(ctx, `SELECT m.business_day_id, m.created_at, m.staff_id, COALESCE(st.name, ''),
		m.approved_by, COALESCE(a.name, ''), m.amount_fils, m.reason
		FROM cash_movements m LEFT JOIN staff st ON st.id = m.staff_id LEFT JOIN staff a ON a.id = m.approved_by
		WHERE m.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, errNotFound
	}
	r := rows.Values[0]
	return &api.CashMovementEvent{Id: uid(id), BusinessDayId: uid(r[0]), CreatedAt: parseTS(r[1]).UTC(),
		StaffId: uid(r[2]), StaffName: str(r[3]), ApprovedBy: uid(r[4]), ApprovedByName: str(r[5]),
		AmountFils: i64(r[6]), Reason: str(r[7])}, nil
}
