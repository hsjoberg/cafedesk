package store

import (
	"context"
	"math"
	"sort"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Usage sets what was bought of each ingredient between two dates against
// what sales used by the recipes: an item's lines times how many sold, a
// batch's portions broken into its ingredients, and options' adjustments.
// Today's recipes, as no history of them is kept. Refunded items count as
// used, as they were made; voided orders don't.
func (s *Store) Usage(ctx context.Context, orgID uuid.UUID, from, to time.Time, shopID *uuid.UUID) (api.Usage, error) {
	out := api.Usage{From: from.Format(dateLayout), To: to.Format(dateLayout), Ingredients: []api.IngredientUsage{}}
	if shopID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM shops WHERE id = $1 AND org_id = $2)", *shopID, orgID).
			Scan(&ok); err != nil {
			return out, err
		}
		if !ok {
			return out, &Problem{ErrNotFound, "There's no such shop."}
		}
	}
	args := []any{orgID, out.From, out.To, shopID}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		b, err := loadBook(ctx, tx, orgID)
		if err != nil {
			return err
		}
		used := map[uuid.UUID]float64{}
		// add puts n of a recipe's lines into used.
		var add func(lines []recipeLine, n float64)
		add = func(lines []recipeLine, n float64) {
			for _, l := range lines {
				amount := n * float64(l.amount) / 1000
				switch {
				case l.ingredient != nil:
					used[*l.ingredient] += amount
				case l.batch != nil:
					if x := b.batches[*l.batch]; x != nil && x.yield > 0 {
						add(x.lines, amount/(float64(x.yield)/1000))
					}
				}
			}
		}

		// Items sold.
		rows, err := tx.Query(ctx, `SELECT l.item_id, SUM(l.qty)::bigint, SUM(l.line_total_fils)::bigint
			FROM order_lines l JOIN orders x ON x.id = l.order_id JOIN business_days d ON d.id = x.business_day_id
			WHERE `+inDays+` AND x.status = 'completed' GROUP BY l.item_id`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item string
			var qty, sales int64
			if err := rows.Scan(&item, &qty, &sales); err != nil {
				return err
			}
			id, perr := uuid.Parse(item)
			if lines := b.items[id]; perr == nil && lines != nil {
				add(lines, float64(qty))
			} else {
				out.UnrecipedFils += sales
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Options chosen, each with its item's quantity.
		rows, err = tx.Query(ctx, `SELECT m.modifier_id, SUM(l.qty)::bigint
			FROM order_line_modifiers m JOIN order_lines l ON l.id = m.line_id
			JOIN orders x ON x.id = l.order_id JOIN business_days d ON d.id = x.business_day_id
			WHERE `+inDays+` AND x.status = 'completed' GROUP BY m.modifier_id`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var mod string
			var qty int64
			if err := rows.Scan(&mod, &qty); err != nil {
				return err
			}
			if id, perr := uuid.Parse(mod); perr == nil {
				if lines := b.modifiers[id]; lines != nil {
					add(lines, float64(qty))
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Bought: sorted products with pack sizes, in the ingredient's unit.
		bought := map[uuid.UUID]float64{}
		rows, err = tx.Query(ctx, `SELECT p.ingredient_id, `+linePack+`, l.qty_milli, l.total_fils
			FROM purchase_lines l JOIN purchases pu ON pu.id = l.purchase_id JOIN products p ON p.id = l.product_id
			WHERE pu.org_id = $1 AND pu.date BETWEEN $2::date AND $3::date AND ($4::uuid IS NULL OR pu.shop_id = $4)
				AND pu.deleted_at IS NULL`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var ing *uuid.UUID
			var pack *int64
			var qty, total int64
			if err := rows.Scan(&ing, &pack, &qty, &total); err != nil {
				return err
			}
			if ing == nil || pack == nil || b.name[*ing] == "" {
				out.UnsortedFils += total
				continue
			}
			bought[*ing] += float64(qty) / 1000 * float64(*pack) / 1000
			out.BoughtFils += total
		}
		if err := rows.Err(); err != nil {
			return err
		}

		units := map[uuid.UUID]api.Unit{}
		urows, err := tx.Query(ctx, "SELECT id, unit FROM ingredients WHERE org_id = $1", orgID)
		if err != nil {
			return err
		}
		for urows.Next() {
			var id uuid.UUID
			var u api.Unit
			if err := urows.Scan(&id, &u); err != nil {
				return err
			}
			units[id] = u
		}
		if err := urows.Err(); err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		var usedFils float64
		for _, m := range []map[uuid.UUID]float64{bought, used} {
			for id := range m {
				if seen[id] || b.name[id] == "" {
					continue
				}
				seen[id] = true
				x := api.IngredientUsage{IngredientId: id, Name: b.name[id], Unit: units[id],
					Bought: round3(bought[id]), Used: round3(used[id]), UnitCostFils: b.price[id]}
				if x.UnitCostFils != nil {
					usedFils += used[id] * *x.UnitCostFils
				}
				out.Ingredients = append(out.Ingredients, x)
			}
		}
		out.UsedFils = int64(math.Round(usedFils))
		return nil
	})
	// The largest difference in money first; without a price, by name, last.
	gap := func(x api.IngredientUsage) float64 {
		if x.UnitCostFils == nil {
			return -1
		}
		return math.Abs(x.Bought-x.Used) * *x.UnitCostFils
	}
	sort.SliceStable(out.Ingredients, func(i, j int) bool {
		gi, gj := gap(out.Ingredients[i]), gap(out.Ingredients[j])
		if gi != gj {
			return gi > gj
		}
		return out.Ingredients[i].Name < out.Ingredients[j].Name
	})
	return out, err
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }
