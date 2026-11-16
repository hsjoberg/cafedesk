package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DemoStaff is someone SeedDemoSales sells as.
type DemoStaff struct {
	ID   uuid.UUID
	Name string
}

// SeedDemoSales adds two tills to a shop, as if connected, and the given
// number of days of made-up sales on them, up to now in the shop's time:
// business days with their cash counts (the odd one short or over), orders
// from the organization's menu shaped like a cafeteria's day, and a few
// voids and refunds. The manager opens and closes the days; the cashiers
// sell. It is for showing and trying the admin, never for a real shop.
func (s *Store) SeedDemoSales(ctx context.Context, orgID, shopID uuid.UUID, manager DemoStaff, cashiers []DemoStaff,
	days int, now time.Time, seed uint64) error {
	cat, err := s.Catalog(ctx, orgID)
	if err != nil {
		return err
	}
	if len(cat.Items) == 0 || len(cashiers) == 0 {
		return fmt.Errorf("demo sales need a menu and someone to sell")
	}
	var tz string
	if err := s.pool.QueryRow(ctx, "SELECT timezone FROM shops WHERE id = $1 AND org_id = $2", shopID, orgID).Scan(&tz); err != nil {
		return err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x5eed))
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// Tea sells most; meals bring the most money.
	weights := make([]int, len(cat.Items))
	total := 0
	for i, it := range cat.Items {
		weights[i] = 2
		if it.PriceFils <= 300 {
			weights[i] = 8
		}
		total += weights[i]
	}
	pick := func() api.Item {
		r := rng.IntN(total)
		for i, w := range weights {
			if r -= w; r < 0 {
				return cat.Items[i]
			}
		}
		return cat.Items[0]
	}
	// Orders an hour, on the front till, from opening at six.
	shape := map[int]int{6: 3, 7: 9, 8: 11, 9: 6, 10: 3, 11: 4, 12: 8, 13: 9, 14: 4, 15: 3, 16: 4, 17: 7, 18: 8,
		19: 7, 20: 6, 21: 5, 22: 3, 23: 1}

	return s.inTx(ctx, func(tx pgx.Tx) error {
		tills := []struct {
			name  string
			scale float64
			seen  time.Duration
		}{{"Front counter", 1, 0}, {"Kitchen", 0.25, 3 * time.Hour}}
		for i, t := range tills {
			id := newID()
			if _, err := tx.Exec(ctx, `INSERT INTO terminals (id, org_id, shop_id, code, name, paired_at, last_seen_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, orgID, shopID, fmt.Sprintf("T%d", i+1), t.name,
				today.AddDate(0, 0, -days), now.Add(-t.seen)); err != nil {
				return err
			}
			var dayRows, orderRows, lineRows, refundRows, refundLineRows [][]any
			for d := days - 1; d >= 0; d-- {
				date := today.AddDate(0, 0, -d)
				weekend := date.Weekday() == time.Friday || date.Weekday() == time.Saturday
				dayID := newID()
				opened := date.Add(6*time.Hour + 10*time.Minute)
				var cash, refundCash int64
				n := 0
				for h := 6; h <= 23; h++ {
					per := float64(shape[h]) * t.scale * (0.7 + rng.Float64()*0.6)
					if weekend {
						per *= 1.35
					}
					for k := 0; k < int(per+0.5); k++ {
						at := date.Add(time.Duration(h)*time.Hour + time.Duration(rng.IntN(3600))*time.Second)
						if !at.Before(now) {
							continue
						}
						n++
						orderID := newID()
						by := cashiers[rng.IntN(len(cashiers))]
						method := "cash"
						if rng.Float64() < 0.32 {
							method = "card"
						}
						var sum int64
						var firstLine uuid.UUID
						var firstTotal int64
						firstQty := 0
						for l := 0; l < 1+rng.IntN(3); l++ {
							it := pick()
							qty := 1 + rng.IntN(2)
							lineID := newID()
							lt := int64(qty) * it.PriceFils
							if l == 0 {
								firstLine, firstTotal, firstQty = lineID, lt, qty
							}
							sum += lt
							lineRows = append(lineRows, []any{lineID, orderID, orgID, l, it.Id.String(), it.Name, qty,
								it.PriceFils, lt, it.Kitchen})
						}
						status := "completed"
						var voidedAt *time.Time
						var voidedBy *uuid.UUID
						var voidedByName, voidReason *string
						if rng.Float64() < 0.012 {
							status, voidedAt, voidedBy = "voided", &at, &manager.ID
							voidedByName, voidReason = &manager.Name, ptrTo("Wrong order")
						} else if method == "cash" {
							cash += sum
						}
						orderRows = append(orderRows, []any{orderID, orgID, shopID, id, dayID, n,
							fmt.Sprintf("T%d-%03d", i+1, n), at, by.ID, by.Name, sum, method, sum, int64(0), status,
							voidedAt, voidedBy, voidedByName, voidReason})
						if status == "completed" && rng.Float64() < 0.008 {
							refundID := newID()
							refundRows = append(refundRows, []any{refundID, orgID, shopID, id, orderID, dayID,
								at.Add(10 * time.Minute), by.ID, by.Name, manager.ID, manager.Name, "Wrong item", method, firstTotal})
							refundLineRows = append(refundLineRows, []any{refundID, firstLine, firstQty, firstTotal})
							if method == "cash" {
								refundCash += firstTotal
							}
						}
					}
				}
				const float = 20000
				day := pgtype.Date{Time: time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
				row := []any{dayID, orgID, shopID, id, day, opened, manager.ID, manager.Name,
					int64(float), nil, nil, nil, nil}
				if d > 0 { // today stays open
					counted := float + cash - refundCash
					switch r := rng.Float64(); {
					case r < 0.12:
						counted -= int64(rng.IntN(40)+1) * 50
					case r < 0.18:
						counted += 500
					}
					row[9], row[10], row[11], row[12] = date.Add(23*time.Hour+40*time.Minute), manager.ID, manager.Name, counted
				}
				dayRows = append(dayRows, row)
			}
			for _, c := range []struct {
				table string
				cols  []string
				rows  [][]any
			}{
				{"business_days", []string{"id", "org_id", "shop_id", "terminal_id", "date", "opened_at", "opened_by",
					"opened_by_name", "opening_float_fils", "closed_at", "closed_by", "closed_by_name", "counted_cash_fils"}, dayRows},
				{"orders", []string{"id", "org_id", "shop_id", "terminal_id", "business_day_id", "number", "code",
					"created_at", "staff_id", "staff_name", "total_fils", "payment_method", "tendered_fils", "change_fils",
					"status", "voided_at", "voided_by", "voided_by_name", "void_reason"}, orderRows},
				{"order_lines", []string{"id", "order_id", "org_id", "position", "item_id", "name", "qty",
					"unit_price_fils", "line_total_fils", "kitchen"}, lineRows},
				{"refunds", []string{"id", "org_id", "shop_id", "terminal_id", "order_id", "business_day_id", "created_at",
					"staff_id", "staff_name", "approved_by", "approved_by_name", "reason", "method", "total_fils"}, refundRows},
				{"refund_lines", []string{"refund_id", "order_line_id", "qty", "amount_fils"}, refundLineRows},
			} {
				if _, err := tx.CopyFrom(ctx, pgx.Identifier{c.table}, c.cols, pgx.CopyFromRows(c.rows)); err != nil {
					return fmt.Errorf("%s: %w", c.table, err)
				}
			}
		}
		return nil
	})
}

func ptrTo[T any](v T) *T { return &v }

// SeedDemoCosts gives the demo menu made-up costs, from the start: tea and
// drinks at about a quarter of their price, meals at about 40%, extras at
// half their price change. Every fifth item is left without one, to show
// how the admin asks for them.
func (s *Store) SeedDemoCosts(ctx context.Context, orgID uuid.UUID) error {
	cat, err := s.Catalog(ctx, orgID)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		round := func(f float64) int64 { return int64(f/5+0.5) * 5 }
		for i, it := range cat.Items {
			if i%5 == 4 {
				continue
			}
			share := 0.4
			if it.PriceFils <= 300 {
				share = 0.25
			}
			if err := setCost(ctx, tx, itemCosts, orgID, it.Id, ptrTo(round(float64(it.PriceFils)*share))); err != nil {
				return err
			}
		}
		for _, g := range cat.ModifierGroups {
			for _, m := range g.Modifiers {
				if m.PriceDeltaFils > 0 {
					if err := setCost(ctx, tx, modifierCosts, orgID, m.Id, ptrTo(round(float64(m.PriceDeltaFils)*0.5))); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
