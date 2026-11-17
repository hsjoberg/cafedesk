package store

import (
	"context"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// maxOrders is the most orders a list holds.
const maxOrders = 2000

// Orders lists an organization's orders of business dates, newest first,
// with their lines and what was refunded of each, in one shop or all.
func (s *Store) Orders(ctx context.Context, orgID uuid.UUID, from, to time.Time, shopID *uuid.UUID) (api.OrderList, error) {
	out := api.OrderList{Orders: []api.OrderRow{}}
	args := []any{orgID, from.Format(dateLayout), to.Format(dateLayout), shopID, maxOrders + 1}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT x.id, x.code, x.created_at, d.date::text, t.code, x.staff_name, x.payment_method,
				x.total_fils, x.status, x.void_reason, x.voided_by_name,
				COALESCE((SELECT SUM(r.total_fils) FROM refunds r WHERE r.order_id = x.id), 0)::bigint
			FROM orders x JOIN business_days d ON d.id = x.business_day_id JOIN terminals t ON t.id = x.terminal_id
			WHERE `+inDays+` ORDER BY x.created_at DESC, x.number DESC LIMIT $5`, args...)
		if err != nil {
			return err
		}
		at := map[uuid.UUID]int{}
		for rows.Next() {
			var o api.OrderRow
			var status, method string
			if err := rows.Scan(&o.Id, &o.Code, &o.CreatedAt, &o.Date, &o.TillCode, &o.StaffName, &method, &o.TotalFils,
				&status, &o.VoidReason, &o.VoidedByName, &o.RefundedFils); err != nil {
				return err
			}
			if len(out.Orders) == maxOrders {
				out.More = true
				break
			}
			o.Status, o.PaymentMethod, o.Lines = api.OrderRowStatus(status), api.PaymentMethod(method), []api.OrderRowLine{}
			at[o.Id] = len(out.Orders)
			out.Orders = append(out.Orders, o)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(out.Orders) == 0 {
			return err
		}
		ids := make([]uuid.UUID, len(out.Orders))
		for i, o := range out.Orders {
			ids[i] = o.Id
		}
		rows, err = tx.Query(ctx, `SELECT l.order_id, l.name, l.qty, l.line_total_fils,
				COALESCE(ARRAY(SELECT m.name FROM order_line_modifiers m WHERE m.line_id = l.id AND NOT m.is_default
					ORDER BY m.position), '{}')
			FROM order_lines l WHERE l.order_id = ANY($1) ORDER BY l.order_id, l.position`, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			var l api.OrderRowLine
			if err := rows.Scan(&id, &l.Name, &l.Qty, &l.LineTotalFils, &l.Options); err != nil {
				return err
			}
			o := &out.Orders[at[id]]
			o.Lines = append(o.Lines, l)
		}
		return rows.Err()
	})
	return out, err
}
