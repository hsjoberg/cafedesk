package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

// demoProduct is something the demo shop buys: its price, and how many of
// it a purchase takes, from min to max.
type demoProduct struct {
	name     string
	area     api.Area
	price    int64
	min, max int
}

// The weekly wholesale invoice, and what is bought on the way in.
var (
	demoWholesale = []demoProduct{
		{"Evaporated milk, 48 x 170 g", api.Counter, 10500, 3, 4},
		{"Tea dust, 1 kg", api.Counter, 2800, 3, 5},
		{"Sugar, 10 kg", api.Counter, 4500, 1, 2},
		{"Cardamom, 500 g", api.Counter, 6500, 1, 1},
		{"Basmati rice, 20 kg", api.Kitchen, 9500, 2, 3},
		{"Chicken, frozen, 10 kg", api.Kitchen, 8500, 3, 4},
		{"Mutton, 5 kg", api.Kitchen, 18000, 1, 2},
		{"Cooking oil, 18 L", api.Kitchen, 9000, 1, 1},
		{"Onions, 10 kg", api.Kitchen, 1800, 2, 2},
		{"Biryani spice mix, 1 kg", api.Kitchen, 3500, 1, 2},
		{"Paper cups, 8 oz x 1000", api.Packaging, 9000, 1, 1},
		{"Food boxes x 100", api.Packaging, 4500, 1, 2},
		{"Dish soap, 5 L", api.Other, 2500, 1, 1},
	}
	demoOneOff = []demoProduct{
		{"Fresh milk, 2 L", api.Counter, 1100, 4, 8},
		{"Tomatoes, 1 kg", api.Kitchen, 400, 3, 5},
		{"Coriander, bunch", api.Kitchen, 100, 4, 6},
		{"Lemons, 1 kg", api.Counter, 600, 1, 2},
	}
)

// SeedDemoPurchases adds made-up purchases over the given days, up to now
// in the shop's time: a wholesale invoice each week, and a few things
// bought nearby every two or three days.
func (s *Store) SeedDemoPurchases(ctx context.Context, orgID, shopID, userID uuid.UUID, days int, now time.Time, seed uint64) error {
	var tz string
	if err := s.pool.QueryRow(ctx, "SELECT timezone FROM shops WHERE id = $1 AND org_id = $2", shopID, orgID).Scan(&tz); err != nil {
		return err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(seed, seed^0xb0b))
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	lines := func(products []demoProduct) []api.PurchaseLineInput {
		var out []api.PurchaseLineInput
		for _, p := range products {
			qty := p.min + rng.IntN(p.max-p.min+1)
			// Prices move a little from week to week.
			price := p.price + int64(rng.IntN(5)-2)*p.price/50
			price = (price + 12) / 25 * 25 // to the quarter dirham
			out = append(out, api.PurchaseLineInput{ProductName: ptrTo(p.name), Area: p.area, Quantity: float64(qty),
				UnitPriceFils: price, TotalFils: int64(qty) * price})
		}
		return out
	}
	invoice := 1040
	for d := days - 1; d >= 0; d-- {
		date := today.AddDate(0, 0, -d)
		var in *api.PurchaseInput
		switch {
		case date.Weekday() == time.Monday:
			invoice++
			in = &api.PurchaseInput{SupplierName: ptrTo("Demo Wholesale"), Reference: ptrTo(fmt.Sprintf("INV-%d", invoice)),
				Lines: lines(demoWholesale)}
		case d%3 == 1:
			picked := []demoProduct{}
			for _, p := range demoOneOff {
				if rng.IntN(3) > 0 {
					picked = append(picked, p)
				}
			}
			if len(picked) == 0 {
				picked = demoOneOff[:1]
			}
			in = &api.PurchaseInput{Lines: lines(picked)}
		default:
			continue
		}
		in.ShopId, in.Date = shopID, date.Format("2006-01-02")
		if _, err := s.CreatePurchase(ctx, orgID, userID, *in, date.Add(10*time.Hour)); err != nil {
			return fmt.Errorf("purchase on %s: %w", in.Date, err)
		}
	}
	return nil
}
