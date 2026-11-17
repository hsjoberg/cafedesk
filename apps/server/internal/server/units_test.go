package server_test

import (
	"math"
	"testing"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

// An invoice line's unit: a product learns what one of each holds, and
// costs and the bought amounts count each line by its own.
func TestPurchaseUnits(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop := me.Shops[0].Id

	var cheese, coke api.Ingredient
	c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "Mozzarella", Unit: api.G}, &cheese)
	c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "Cola, can", Unit: api.Piece}, &coke)

	buy := func(date string, l api.PurchaseLineInput) (api.Purchase, int) {
		t.Helper()
		var p api.Purchase
		l.Area = api.Kitchen
		l.UnitPriceFils = l.TotalFils
		if l.Quantity == 0 {
			l.Quantity = 1
		}
		code := c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop, Date: date,
			Lines: []api.PurchaseLineInput{l}}, &p)
		return p, code
	}
	product := func(id uuid.UUID) api.Product {
		t.Helper()
		var list []api.Product
		c.do("GET", "/v1/admin/products", nil, &list)
		for _, p := range list {
			if p.Id == id {
				return p
			}
		}
		t.Fatalf("no product %s", id)
		return api.Product{}
	}
	cost := func(id uuid.UUID) float64 {
		t.Helper()
		var list []api.Ingredient
		c.do("GET", "/v1/admin/ingredients", nil, &list)
		for _, x := range list {
			if x.Id == id && x.UnitCostFils != nil {
				return math.Round(*x.UnitCostFils*1000) / 1000
			}
		}
		return -1
	}

	// One bag, sorted as it is first bought: one EA holds 2 kg.
	p, code := buy("2026-11-02", api.PurchaseLineInput{ProductName: ptr("3 Cow Mozzarella 6x2kg"), Uom: ptr(" ea "),
		TotalFils: 5000, IngredientId: &cheese.Id, PackAmount: ptr(2000.0)})
	if code != 201 || p.Lines[0].Uom != "EA" {
		t.Fatalf("a bag: %d %+v", code, p)
	}
	mozz := p.Lines[0].ProductId
	if x := product(mozz); x.PackUom != "EA" || x.PackAmount == nil || *x.PackAmount != 2000 || len(x.Packs) != 0 ||
		x.LastUom == nil || *x.LastUom != "EA" {
		t.Fatalf("after a bag: %+v", x)
	}
	if got := cost(cheese.Id); got != 2.5 {
		t.Errorf("a bag's cost: %v fils a gram", got)
	}
	// Then the carton: it learns what one CAR holds.
	if _, code := buy("2026-11-03", api.PurchaseLineInput{ProductName: ptr("3 Cow Mozzarella 6x2kg"), Uom: ptr("CAR"),
		TotalFils: 24000, PackAmount: ptr(12000.0)}); code != 201 {
		t.Fatalf("a carton: %d", code)
	}
	if x := product(mozz); len(x.Packs) != 1 || x.Packs[0].Uom != "CAR" || x.Packs[0].PackAmount != 12000 ||
		x.PackUom != "EA" || *x.PackAmount != 2000 {
		t.Fatalf("after a carton: %+v", x)
	}
	// 5000 + 24000 fils for 2 + 12 kg.
	if got := cost(cheese.Id); got != math.Round(29000.0/14000*1000)/1000 {
		t.Errorf("a bag and a carton: %v", got)
	}
	// Known units need nothing more; an unknown one, with no size, isn't counted.
	buy("2026-11-04", api.PurchaseLineInput{ProductId: &mozz, Uom: ptr("car"), Quantity: 2, TotalFils: 48000})
	if got := cost(cheese.Id); got != math.Round(77000.0/38000*1000)/1000 {
		t.Errorf("two cartons more: %v", got)
	}
	buy("2026-11-05", api.PurchaseLineInput{ProductId: &mozz, Uom: ptr("BOX"), TotalFils: 99999})
	if got := cost(cheese.Id); got != math.Round(77000.0/38000*1000)/1000 {
		t.Errorf("a box of no known size counted: %v", got)
	}
	var usage api.Usage
	c.do("GET", "/v1/admin/reports/usage?from=2026-11-01&to=2026-11-30", nil, &usage)
	if usage.UnsortedFils != 99999 {
		t.Errorf("usage: bought %d, not counted %d", usage.BoughtFils, usage.UnsortedFils)
	}

	// A pack size given with no unit is the unit's a line first gives it,
	// unless the line says one holds something else.
	p, _ = buy("2026-11-02", api.PurchaseLineInput{ProductName: ptr("Cola Can 24x330ml"), TotalFils: 3900,
		IngredientId: &coke.Id, PackAmount: ptr(24.0)})
	cola := p.Lines[0].ProductId
	if x := product(cola); x.PackUom != "" || *x.PackAmount != 24 {
		t.Fatalf("no unit: %+v", x)
	}
	buy("2026-11-03", api.PurchaseLineInput{ProductId: &cola, Uom: ptr("CAR"), TotalFils: 3900})
	if x := product(cola); x.PackUom != "CAR" || len(x.Packs) != 0 {
		t.Fatalf("learnt CAR: %+v", x)
	}
	buy("2026-11-04", api.PurchaseLineInput{ProductId: &cola, Uom: ptr("EA"), TotalFils: 200, PackAmount: ptr(1.0)})
	if x := product(cola); len(x.Packs) != 1 || x.Packs[0].Uom != "EA" || x.Packs[0].PackAmount != 1 {
		t.Fatalf("learnt EA: %+v", x)
	}

	// A unit is short, and a product's units are each named once.
	var e api.Error
	if _, code := buy("2026-11-06", api.PurchaseLineInput{ProductId: &cola, Uom: ptr("CARTON-OF"), TotalFils: 1}); code != 400 {
		t.Errorf("a long unit: %d", code)
	}
	in := api.ProductInput{Name: "Cola Can 24x330ml", Area: api.Counter, IngredientId: &coke.Id, PackAmount: ptr(24.0),
		PackUom: ptr("CAR"), Packs: &[]api.ProductPack{{Uom: "ea", PackAmount: 1}, {Uom: "EA", PackAmount: 2}}}
	if code := c.do("PUT", "/v1/admin/products/"+cola.String(), in, &e); code != 400 {
		t.Errorf("a unit twice: %d", code)
	}
	in.Packs = &[]api.ProductPack{{Uom: "PAC", PackAmount: 6}}
	var x api.Product
	if code := c.do("PUT", "/v1/admin/products/"+cola.String(), in, &x); code != 200 || len(x.Packs) != 1 ||
		x.Packs[0].Uom != "PAC" || x.PackUom != "CAR" {
		t.Errorf("units replaced: %d %+v", code, x)
	}
}
