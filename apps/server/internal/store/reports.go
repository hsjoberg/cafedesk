package store

import (
	"context"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The reports count a sale or refund on the business day the till had open
// when it happened, as the till's own day report does, so the two agree. A
// day the till opened at 7 in the morning and closed after midnight is one
// date. Hours are the clock in the shop's time zone.

// inDays limits a query to orders or refunds, aliased x, whose business day
// d falls in the report: $1 organization, $2 and $3 the dates, $4 the shop
// or NULL for all.
const inDays = `x.org_id = $1 AND d.date BETWEEN $2::date AND $3::date AND ($4::uuid IS NULL OR x.shop_id = $4)`

const dateLayout = "2006-01-02"

// SalesReport sums an organization's sales from one business date to
// another, both included, in one shop or all of them.
func (s *Store) SalesReport(ctx context.Context, orgID uuid.UUID, from, to time.Time, shopID *uuid.UUID) (api.SalesReport, error) {
	rep := api.SalesReport{From: from.Format(dateLayout), To: to.Format(dateLayout),
		Days: []api.DaySales{}, Hours: make([]api.HourSales, 24), Items: []api.ItemSales{},
		Tills: []api.TillSales{}, Staff: []api.StaffSales{}, Closes: []api.DayClose{}}
	for h := range rep.Hours {
		rep.Hours[h].Hour = h
	}
	if shopID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM shops WHERE id = $1 AND org_id = $2)", *shopID, orgID).
			Scan(&ok); err != nil {
			return rep, err
		}
		if !ok {
			return rep, &Problem{ErrNotFound, "There's no such shop."}
		}
	}
	args := []any{orgID, rep.From, rep.To, shopID}

	// One snapshot, so a till sending meanwhile can't make the parts disagree.
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		t := &rep.Totals
		rows, err := tx.Query(ctx, `SELECT g::date::text,
				COALESCE(o.orders, 0), COALESCE(o.gross, 0), COALESCE(o.cash, 0), COALESCE(o.voids, 0), COALESCE(o.voided, 0),
				COALESCE(r.refunds, 0), COALESCE(r.total, 0), COALESCE(r.cash, 0)
			FROM generate_series($2::date, $3::date, interval '1 day') g
			LEFT JOIN (SELECT d.date,
					COUNT(*) FILTER (WHERE x.status = 'completed') AS orders,
					SUM(x.total_fils) FILTER (WHERE x.status = 'completed')::bigint AS gross,
					SUM(x.total_fils) FILTER (WHERE x.status = 'completed' AND x.payment_method = 'cash')::bigint AS cash,
					COUNT(*) FILTER (WHERE x.status = 'voided') AS voids,
					SUM(x.total_fils) FILTER (WHERE x.status = 'voided')::bigint AS voided
				FROM orders x JOIN business_days d ON d.id = x.business_day_id
				WHERE `+inDays+` GROUP BY d.date) o ON o.date = g::date
			LEFT JOIN (SELECT d.date, COUNT(*) AS refunds, SUM(x.total_fils)::bigint AS total,
					SUM(x.total_fils) FILTER (WHERE x.method = 'cash')::bigint AS cash
				FROM refunds x JOIN business_days d ON d.id = x.business_day_id
				WHERE `+inDays+` GROUP BY d.date) r ON r.date = g::date
			ORDER BY g`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var day api.DaySales
			var cash, voided, refundsCash int64
			var voids, refunds int
			if err := rows.Scan(&day.Date, &day.Orders, &day.GrossFils, &cash, &voids, &voided, &refunds, &day.RefundsFils, &refundsCash); err != nil {
				return err
			}
			day.NetFils = day.GrossFils - day.RefundsFils
			rep.Days = append(rep.Days, day)
			t.Orders += day.Orders
			t.GrossFils += day.GrossFils
			t.CashFils += cash
			t.Voids += voids
			t.VoidsFils += voided
			t.Refunds += refunds
			t.RefundsFils += day.RefundsFils
			t.RefundsCashFils += refundsCash
		}
		if err := rows.Err(); err != nil {
			return err
		}
		t.CardFils = t.GrossFils - t.CashFils
		t.NetFils = t.GrossFils - t.RefundsFils

		rows, err = tx.Query(ctx, `SELECT extract(hour FROM x.created_at AT TIME ZONE sh.timezone)::int,
				COUNT(*), SUM(x.total_fils)::bigint
			FROM orders x JOIN business_days d ON d.id = x.business_day_id JOIN shops sh ON sh.id = x.shop_id
			WHERE `+inDays+` AND x.status = 'completed' GROUP BY 1`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var h api.HourSales
			if err := rows.Scan(&h.Hour, &h.Orders, &h.GrossFils); err != nil {
				return err
			}
			rep.Hours[h.Hour] = h
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT l.item_id, (array_agg(l.name ORDER BY x.created_at DESC))[1],
				SUM(l.qty)::int, SUM(l.line_total_fils)::bigint
			FROM order_lines l JOIN orders x ON x.id = l.order_id JOIN business_days d ON d.id = x.business_day_id
			WHERE `+inDays+` AND x.status = 'completed'
			GROUP BY l.item_id ORDER BY 4 DESC, 3 DESC, 2`, args...)
		if err != nil {
			return err
		}
		rep.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.ItemSales, error) {
			var it api.ItemSales
			err := r.Scan(&it.ItemId, &it.Name, &it.Qty, &it.GrossFils)
			return it, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT t.id, t.code, t.name, COALESCE(o.orders, 0), COALESCE(o.gross, 0), COALESCE(r.total, 0)
			FROM terminals t
			LEFT JOIN (SELECT x.terminal_id, COUNT(*) AS orders, SUM(x.total_fils)::bigint AS gross
				FROM orders x JOIN business_days d ON d.id = x.business_day_id
				WHERE `+inDays+` AND x.status = 'completed' GROUP BY 1) o ON o.terminal_id = t.id
			LEFT JOIN (SELECT x.terminal_id, SUM(x.total_fils)::bigint AS total
				FROM refunds x JOIN business_days d ON d.id = x.business_day_id
				WHERE `+inDays+` GROUP BY 1) r ON r.terminal_id = t.id
			WHERE t.org_id = $1 AND (o.orders IS NOT NULL OR r.total IS NOT NULL)
			ORDER BY length(t.code), t.code`, args...)
		if err != nil {
			return err
		}
		rep.Tills, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.TillSales, error) {
			var ts api.TillSales
			err := r.Scan(&ts.TerminalId, &ts.Code, &ts.Name, &ts.Orders, &ts.GrossFils, &ts.RefundsFils)
			ts.NetFils = ts.GrossFils - ts.RefundsFils
			return ts, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT x.staff_id, (array_agg(x.staff_name ORDER BY x.created_at DESC))[1],
				COUNT(*), SUM(x.total_fils)::bigint
			FROM orders x JOIN business_days d ON d.id = x.business_day_id
			WHERE `+inDays+` AND x.status = 'completed'
			GROUP BY x.staff_id ORDER BY 4 DESC, 2`, args...)
		if err != nil {
			return err
		}
		rep.Staff, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.StaffSales, error) {
			var st api.StaffSales
			err := r.Scan(&st.StaffId, &st.Name, &st.Orders, &st.GrossFils)
			return st, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT x.id, x.date::text, t.code, t.name, x.opened_at, x.opened_by_name,
				x.closed_at, x.closed_by_name, x.opening_float_fils, x.counted_cash_fils,
				COALESCE((SELECT SUM(o.total_fils) FROM orders o
					WHERE o.business_day_id = x.id AND o.status = 'completed' AND o.payment_method = 'cash'), 0)::bigint,
				COALESCE((SELECT SUM(r.total_fils) FROM refunds r
					WHERE r.business_day_id = x.id AND r.method = 'cash'), 0)::bigint
			FROM business_days x JOIN terminals t ON t.id = x.terminal_id
			WHERE x.org_id = $1 AND x.date BETWEEN $2::date AND $3::date AND ($4::uuid IS NULL OR x.shop_id = $4)
			ORDER BY x.date DESC, length(t.code), t.code, x.opened_at DESC`, args...)
		if err != nil {
			return err
		}
		rep.Closes, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.DayClose, error) {
			var c api.DayClose
			err := r.Scan(&c.Id, &c.Date, &c.TerminalCode, &c.TerminalName, &c.OpenedAt, &c.OpenedByName,
				&c.ClosedAt, &c.ClosedByName, &c.FloatFils, &c.CountedFils, &c.CashSalesFils, &c.CashRefundsFils)
			c.ExpectedFils = c.FloatFils + c.CashSalesFils - c.CashRefundsFils
			if c.ClosedAt != nil && c.CountedFils != nil {
				d := *c.CountedFils - c.ExpectedFils
				c.DifferenceFils = &d
			} else {
				c.CountedFils = nil
			}
			return c, err
		})
		return err
	})
	return rep, err
}
