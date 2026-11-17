package store

import (
	"context"
	"errors"
	"fmt"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Changes returns the menu and the shop's staff changed after version
// since. From 0 it is everything not deleted: the till then drops what it
// has that isn't in it.
func (s *Store) Changes(ctx context.Context, t Terminal, since int64) (api.Changes, error) {
	ch := api.Changes{Full: since == 0, Categories: []api.SyncCategory{}, Items: []api.SyncItem{},
		ModifierGroups: []api.SyncModifierGroup{}, Staff: []api.SyncStaff{}}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Read the version first, in the transaction: rows changed after it
		// come next time, never not at all.
		if err := tx.QueryRow(ctx, "SELECT catalog_version FROM organizations WHERE id = $1", t.OrgID).Scan(&ch.Version); err != nil {
			return err
		}
		live := "AND NOT archived"
		if !ch.Full {
			live = ""
		}

		rows, err := tx.Query(ctx, `SELECT id, name, name_ar, color, position, kitchen, archived FROM categories
			WHERE org_id = $1 AND version > $2 AND version <= $3 `+live+` ORDER BY position`, t.OrgID, since, ch.Version)
		if err != nil {
			return err
		}
		ch.Categories, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.SyncCategory, error) {
			var c api.SyncCategory
			err := r.Scan(&c.Id, &c.Name, &c.NameAr, &c.Color, &c.Position, &c.Kitchen, &c.Archived)
			return c, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT i.id, i.category_id, i.name, i.name_ar, i.price_fils, i.position, i.kitchen, i.archived,
			COALESCE((SELECT array_agg(img.group_id ORDER BY img.position) FROM item_modifier_groups img
				JOIN modifier_groups g ON g.id = img.group_id AND NOT g.archived WHERE img.item_id = i.id), '{}')
			FROM items i WHERE i.org_id = $1 AND i.version > $2 AND i.version <= $3 `+live+` ORDER BY i.position`,
			t.OrgID, since, ch.Version)
		if err != nil {
			return err
		}
		ch.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.SyncItem, error) {
			var it api.SyncItem
			err := r.Scan(&it.Id, &it.CategoryId, &it.Name, &it.NameAr, &it.PriceFils, &it.Position, &it.Kitchen,
				&it.Archived, &it.ModifierGroupIds)
			return it, err
		})
		if err != nil {
			return err
		}

		// A group comes whole, with all its modifiers, when it or any of them
		// changed.
		rows, err = tx.Query(ctx, `SELECT id, name, name_ar, multiple, position, archived FROM modifier_groups g
			WHERE org_id = $1 `+live+` AND ((version > $2 AND version <= $3)
				OR EXISTS (SELECT 1 FROM modifiers m WHERE m.group_id = g.id AND m.version > $2 AND m.version <= $3))
			ORDER BY position`, t.OrgID, since, ch.Version)
		if err != nil {
			return err
		}
		ch.ModifierGroups, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.SyncModifierGroup, error) {
			var g api.SyncModifierGroup
			err := r.Scan(&g.Id, &g.Name, &g.NameAr, &g.Multiple, &g.Position, &g.Archived)
			g.Modifiers = []api.SyncModifier{}
			return g, err
		})
		if err != nil {
			return err
		}
		for i := range ch.ModifierGroups {
			g := &ch.ModifierGroups[i]
			rows, err := tx.Query(ctx, `SELECT id, name, name_ar, price_delta_fils, is_default, position, archived
				FROM modifiers WHERE group_id = $1 `+live+` ORDER BY position`, g.Id)
			if err != nil {
				return err
			}
			g.Modifiers, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.SyncModifier, error) {
				var m api.SyncModifier
				err := r.Scan(&m.Id, &m.Name, &m.NameAr, &m.PriceDeltaFils, &m.IsDefault, &m.Position, &m.Archived)
				return m, err
			})
			if err != nil {
				return err
			}
		}

		// Staff: everyone of the organization who changed; active only if
		// they may sign in to this shop's tills.
		rows, err = tx.Query(ctx, `SELECT s.id, s.name, s.role, s.pin_hash, s.must_change_pin,
			s.active AND EXISTS (SELECT 1 FROM staff_shops ss WHERE ss.staff_id = s.id AND ss.shop_id = $4)
			FROM staff s WHERE s.org_id = $1 AND s.version > $2 AND s.version <= $3 ORDER BY s.name`,
			t.OrgID, since, ch.Version, t.ShopID)
		if err != nil {
			return err
		}
		ch.Staff, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.SyncStaff, error) {
			var st api.SyncStaff
			var role string
			err := r.Scan(&st.Id, &st.Name, &role, &st.PinHash, &st.MustChangePin, &st.Active)
			st.Role = api.StaffRole(role)
			return st, err
		})
		if ch.Full {
			// Leave out those who were never on this shop's tills.
			kept := ch.Staff[:0]
			for _, st := range ch.Staff {
				if st.Active {
					kept = append(kept, st)
				}
			}
			ch.Staff = kept
		}
		return err
	})
	return ch, err
}

// ErrRejected wraps why an event can't be taken.
var ErrRejected = errors.New("rejected")

func reject(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrRejected}, args...)...)
}

// ApplyEvent takes one event from a till, in a transaction. Taking an
// event again changes nothing.
func (s *Store) ApplyEvent(ctx context.Context, t Terminal, ev api.Event) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		switch ev.Type {
		case api.EventTypeBusinessDay:
			if ev.BusinessDay == nil {
				return reject("a business_day event needs businessDay")
			}
			return applyDay(ctx, tx, t, *ev.BusinessDay)
		case api.EventTypeOrder:
			if ev.Order == nil {
				return reject("an order event needs order")
			}
			return applyOrder(ctx, tx, t, *ev.Order)
		case api.EventTypeOrderVoid:
			if ev.OrderVoid == nil {
				return reject("an order_void event needs orderVoid")
			}
			return applyVoid(ctx, tx, t, *ev.OrderVoid)
		case api.EventTypeRefund:
			if ev.Refund == nil {
				return reject("a refund event needs refund")
			}
			return applyRefund(ctx, tx, t, *ev.Refund)
		case api.EventTypeStaffPin:
			if ev.StaffPin == nil {
				return reject("a staff_pin event needs staffPin")
			}
			return applyStaffPIN(ctx, tx, t, *ev.StaffPin)
		case api.EventTypeCashMovement:
			if ev.CashMovement == nil {
				return reject("a cash_movement event needs cashMovement")
			}
			return applyCashMovement(ctx, tx, t, *ev.CashMovement)
		}
		return reject("unknown event type %q", ev.Type)
	})
}

func applyDay(ctx context.Context, tx pgx.Tx, t Terminal, d api.BusinessDayEvent) error {
	tag, err := tx.Exec(ctx, `INSERT INTO business_days (id, org_id, shop_id, terminal_id, date, opened_at, opened_by,
		opened_by_name, opening_float_fils, closed_at, closed_by, closed_by_name, counted_cash_fils)
		VALUES ($1, $2, $3, $4, $5::date, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET closed_at = COALESCE(EXCLUDED.closed_at, business_days.closed_at),
			closed_by = COALESCE(EXCLUDED.closed_by, business_days.closed_by),
			closed_by_name = COALESCE(EXCLUDED.closed_by_name, business_days.closed_by_name),
			counted_cash_fils = COALESCE(EXCLUDED.counted_cash_fils, business_days.counted_cash_fils)
		WHERE business_days.shop_id = EXCLUDED.shop_id`,
		d.Id, t.OrgID, t.ShopID, t.ID, d.Date, d.OpenedAt, d.OpenedBy, d.OpenedByName, d.OpeningFloatFils,
		d.ClosedAt, d.ClosedBy, d.ClosedByName, d.CountedCashFils)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return reject("business day %s belongs to another shop", d.Id)
	}
	return nil
}

// dayOfShop makes sure a business day was sent, by a till of this shop.
func dayOfShop(ctx context.Context, tx pgx.Tx, t Terminal, id uuid.UUID) error {
	var ok bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM business_days WHERE id = $1 AND shop_id = $2)", id, t.ShopID).
		Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return reject("business day %s hasn't been sent", id)
	}
	return nil
}

func applyOrder(ctx context.Context, tx pgx.Tx, t Terminal, o api.OrderEvent) error {
	if err := dayOfShop(ctx, tx, t, o.BusinessDayId); err != nil {
		return err
	}
	var sum int64
	for _, l := range o.Lines {
		sum += l.LineTotalFils
	}
	if sum != o.TotalFils {
		return reject("order %s: its lines add up to %d, not its total %d", o.Code, sum, o.TotalFils)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO orders (id, org_id, shop_id, terminal_id, business_day_id, number, code,
		created_at, staff_id, staff_name, total_fils, payment_method, tendered_fils, change_fils)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (id) DO NOTHING`,
		o.Id, t.OrgID, t.ShopID, t.ID, o.BusinessDayId, o.Number, o.Code, o.CreatedAt, o.StaffId, o.StaffName,
		o.TotalFils, string(o.PaymentMethod), o.TenderedFils, o.ChangeFils)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // taken before
	}
	for i, l := range o.Lines {
		if _, err := tx.Exec(ctx, `INSERT INTO order_lines (id, order_id, org_id, position, item_id, name, qty,
			unit_price_fils, line_total_fils, kitchen) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			l.Id, o.Id, t.OrgID, i, l.ItemId, l.Name, l.Qty, l.UnitPriceFils, l.LineTotalFils, l.Kitchen); err != nil {
			return err
		}
		for j, m := range l.Modifiers {
			if _, err := tx.Exec(ctx, `INSERT INTO order_line_modifiers (line_id, order_id, position, group_id, group_name,
				modifier_id, name, price_delta_fils, is_default) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				l.Id, o.Id, j, m.GroupId, m.GroupName, m.ModifierId, m.Name, m.PriceDeltaFils, m.IsDefault); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyVoid(ctx context.Context, tx pgx.Tx, t Terminal, v api.OrderVoidEvent) error {
	var status string
	err := tx.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1 AND shop_id = $2 FOR UPDATE", v.OrderId, t.ShopID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return reject("order %s hasn't been sent", v.OrderId)
	}
	if err != nil || status == "voided" {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET status = 'voided', voided_at = $2, voided_by = $3, voided_by_name = $4,
		void_reason = $5 WHERE id = $1`, v.OrderId, v.VoidedAt, v.VoidedBy, v.VoidedByName, v.Reason)
	return err
}

func applyRefund(ctx context.Context, tx pgx.Tx, t Terminal, r api.RefundEvent) error {
	var ok bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM orders WHERE id = $1 AND shop_id = $2)", r.OrderId, t.ShopID).
		Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return reject("order %s hasn't been sent", r.OrderId)
	}
	if err := dayOfShop(ctx, tx, t, r.BusinessDayId); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO refunds (id, org_id, shop_id, terminal_id, order_id, business_day_id, created_at,
		staff_id, staff_name, approved_by, approved_by_name, reason, method, total_fils)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (id) DO NOTHING`,
		r.Id, t.OrgID, t.ShopID, t.ID, r.OrderId, r.BusinessDayId, r.CreatedAt, r.StaffId, r.StaffName,
		r.ApprovedBy, r.ApprovedByName, r.Reason, string(r.Method), r.TotalFils)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	for _, l := range r.Lines {
		if _, err := tx.Exec(ctx, `INSERT INTO refund_lines (refund_id, order_line_id, qty, amount_fils)
			SELECT $1, $2, $3, $4 WHERE EXISTS (SELECT 1 FROM order_lines WHERE id = $2 AND order_id = $5)`,
			r.Id, l.OrderLineId, l.Qty, l.AmountFils, r.OrderId); err != nil {
			return err
		}
	}
	return nil
}

func applyCashMovement(ctx context.Context, tx pgx.Tx, t Terminal, m api.CashMovementEvent) error {
	if m.AmountFils == 0 {
		return reject("cash movement %s has no amount", m.Id)
	}
	if err := dayOfShop(ctx, tx, t, m.BusinessDayId); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO cash_movements (id, org_id, shop_id, terminal_id, business_day_id, created_at,
		staff_id, staff_name, approved_by, approved_by_name, amount_fils, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) ON CONFLICT (id) DO NOTHING`,
		m.Id, t.OrgID, t.ShopID, t.ID, m.BusinessDayId, m.CreatedAt, m.StaffId, m.StaffName,
		m.ApprovedBy, m.ApprovedByName, m.AmountFils, m.Reason)
	return err
}

// applyStaffPIN takes a PIN someone chose at a till, if their PIN is still
// the one the till replaced: a starting PIN set in the admin meanwhile wins,
// whatever the clocks say. Someone the server doesn't know, from before the
// till was paired, is passed over.
func applyStaffPIN(ctx context.Context, tx pgx.Tx, t Terminal, p api.StaffPinEvent) error {
	var current bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM staff s JOIN staff_shops ss ON ss.staff_id = s.id
		WHERE s.id = $1 AND s.org_id = $2 AND ss.shop_id = $3 AND s.pin_hash = $4)`,
		p.StaffId, t.OrgID, t.ShopID, p.PreviousPinHash).Scan(&current)
	if err != nil || !current {
		return err
	}
	v, err := bump(ctx, tx, t.OrgID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE staff SET pin_hash = $2, must_change_pin = false, version = $3, updated_at = now()
		WHERE id = $1 AND pin_hash = $4`, p.StaffId, p.PinHash, v, p.PreviousPinHash)
	return err
}
