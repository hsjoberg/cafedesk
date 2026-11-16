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
	// The ingredient it is, its unit, and how much of it one holds; none
	// for what recipes don't use.
	ingredient string
	unit       api.Unit
	pack       float64
}

// The weekly wholesale invoice, and what is bought on the way in.
var (
	demoWholesale = []demoProduct{
		{"Evaporated milk, 48 x 170 g", api.Counter, 10500, 3, 4, "Evaporated milk", api.G, 8160},
		{"Tea dust, 1 kg", api.Counter, 2800, 3, 5, "Tea dust", api.G, 1000},
		{"Sugar, 10 kg", api.Counter, 4500, 1, 2, "Sugar", api.G, 10000},
		{"Cardamom, 500 g", api.Counter, 6500, 1, 1, "Cardamom", api.G, 500},
		{"Basmati rice, 20 kg", api.Kitchen, 9500, 2, 3, "Basmati rice", api.G, 20000},
		{"Chicken, frozen, 10 kg", api.Kitchen, 8500, 3, 4, "Chicken", api.G, 10000},
		{"Mutton, 5 kg", api.Kitchen, 18000, 1, 2, "Mutton", api.G, 5000},
		{"Cooking oil, 18 L", api.Kitchen, 9000, 1, 1, "Cooking oil", api.Ml, 18000},
		{"Onions, 10 kg", api.Kitchen, 1800, 2, 2, "Onions", api.G, 10000},
		{"Biryani spice mix, 1 kg", api.Kitchen, 3500, 1, 2, "Biryani spice mix", api.G, 1000},
		{"Paper cups, 8 oz x 1000", api.Packaging, 9000, 1, 1, "Paper cup", api.Piece, 1000},
		{"Food boxes x 100", api.Packaging, 4500, 1, 2, "Food box", api.Piece, 100},
		{"Dish soap, 5 L", api.Other, 2500, 1, 1, "", "", 0},
	}
	demoOneOff = []demoProduct{
		{"Fresh milk, 2 L", api.Counter, 1100, 4, 8, "Fresh milk", api.Ml, 2000},
		{"Tomatoes, 1 kg", api.Kitchen, 400, 3, 5, "Tomatoes", api.G, 1000},
		{"Coriander, bunch", api.Kitchen, 100, 4, 6, "Coriander", api.Piece, 1},
		{"Lemons, 1 kg", api.Counter, 600, 1, 2, "", "", 0},
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
			// As wholesale invoices come: item codes, prices before 5% VAT.
			ls := lines(demoWholesale)
			for j := range ls {
				l := &ls[j]
				before := l.TotalFils
				vat := (before*5 + 50) / 100
				l.TotalFils, l.VatFils, l.ProductCode = before+vat, ptrTo(vat), ptrTo(fmt.Sprint(1002600+j*17))
			}
			in = &api.PurchaseInput{SupplierName: ptrTo("Demo Wholesale"), Reference: ptrTo(fmt.Sprintf("INV-%d", invoice)),
				PricesBeforeVat: ptrTo(true), Lines: ls}
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

// SeedDemoIngredients sorts the demo's products into ingredients, with
// their pack sizes. Dish soap and lemons are left unsorted, to show how
// the admin asks for them.
func (s *Store) SeedDemoIngredients(ctx context.Context, orgID uuid.UUID) error {
	products, err := s.ListProducts(ctx, orgID)
	if err != nil {
		return err
	}
	ids := map[string]uuid.UUID{}
	for _, p := range products {
		ids[p.Name] = p.Id
	}
	for _, p := range append(append([]demoProduct{}, demoWholesale...), demoOneOff...) {
		id, bought := ids[p.name]
		if p.ingredient == "" || !bought {
			continue
		}
		ing, err := s.CreateIngredient(ctx, orgID, api.IngredientInput{Name: p.ingredient, Unit: p.unit})
		if err != nil {
			return err
		}
		if _, err := s.UpdateProduct(ctx, orgID, id, api.ProductInput{Name: p.name, Area: p.area,
			IngredientId: &ing.Id, PackAmount: ptrTo(p.pack)}); err != nil {
			return err
		}
	}
	return nil
}
