package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Costs are kept as a history, never overwritten: see migration 0005. The
// IDs are UUIDv7, so the latest of two rows with the same effective_from is
// the one with the greater ID.

// costTable is item_costs or modifier_costs, with its key column.
type costTable struct{ table, key string }

var (
	itemCosts     = costTable{"item_costs", "item_id"}
	modifierCosts = costTable{"modifier_costs", "modifier_id"}
)

// setCost records a new cost for an item or option, if it differs from the
// current one. The first cost applies from the start; a change, from now,
// by this server's clock: sales carry their tills' times, not the
// database's, so the database's clock means nothing to them.
func setCost(ctx context.Context, tx pgx.Tx, t costTable, orgID, id uuid.UUID, cost *int64) error {
	var current *int64
	err := tx.QueryRow(ctx, `SELECT cost_fils FROM `+t.table+` WHERE `+t.key+` = $1
		ORDER BY effective_from DESC, id DESC LIMIT 1`, id).Scan(&current)
	first := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !first {
		return err
	}
	if first && cost == nil {
		return nil
	}
	if !first && ((current == nil && cost == nil) || (current != nil && cost != nil && *current == *cost)) {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+t.table+` (id, org_id, `+t.key+`, cost_fils, effective_from)
		VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN '-infinity'::timestamptz ELSE $6 END)`,
		newID(), orgID, id, cost, first, time.Now())
	return err
}

// currentCost is a subquery for the current cost of the row aliased as.
func currentCost(t costTable, as string) string {
	return `(SELECT c.cost_fils FROM ` + t.table + ` c WHERE c.` + t.key + ` = ` + as + `.id
		ORDER BY c.effective_from DESC, c.id DESC LIMIT 1)`
}

// costedLines is a WITH clause: the completed order lines in a report's
// days (see inDays), each with its unit cost when it was sold, NULL when
// the item had none, plus what its options cost then (unknown counts as
// nothing). Item and option IDs are text in sales, as a till may have sold
// from its demo menu: only IDs that are UUIDs are looked up.
const costedLines = `WITH lines AS (
	SELECT l.id, l.item_id, l.name, l.qty, l.line_total_fils, x.created_at,
		(SELECT c.cost_fils FROM item_costs c
			WHERE c.item_id = (CASE WHEN l.item_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN l.item_id::uuid END)
			AND c.effective_from <= x.created_at
			ORDER BY c.effective_from DESC, c.id DESC LIMIT 1) AS unit_cost,
		(SELECT COALESCE(SUM(mc.cost_fils), 0) FROM order_line_modifiers m CROSS JOIN LATERAL
			(SELECT c.cost_fils FROM modifier_costs c
				WHERE c.modifier_id = (CASE WHEN m.modifier_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN m.modifier_id::uuid END)
				AND c.effective_from <= x.created_at
				ORDER BY c.effective_from DESC, c.id DESC LIMIT 1) mc
			WHERE m.line_id = l.id)::bigint AS options_cost
	FROM order_lines l JOIN orders x ON x.id = l.order_id JOIN business_days d ON d.id = x.business_day_id
	WHERE ` + inDays + ` AND x.status = 'completed'
), costed AS (
	SELECT lines.*, CASE WHEN unit_cost IS NOT NULL THEN qty * (unit_cost + options_cost) END AS cost
	FROM lines
)`
