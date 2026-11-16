package server_test

import (
	"math"
	"testing"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

func TestIngredients(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop := me.Shops[0].Id

	buy := func(date, product string, qty float64, price int64) api.Purchase {
		t.Helper()
		var p api.Purchase
		if code := c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop, Date: date,
			Lines: []api.PurchaseLineInput{{ProductName: ptr(product), Area: api.Counter, Quantity: qty,
				UnitPriceFils: price, TotalFils: int64(math.Round(qty * float64(price)))}}}, &p); code != 201 {
			t.Fatalf("buying %s: %d", product, code)
		}
		return p
	}
	carton := buy("2026-11-02", "Rainbow evaporated milk, 48 x 170 g", 2, 10500).Lines[0].ProductId
	buy("2026-11-09", "Rainbow evaporated milk, 48 x 170 g", 1, 11000)

	var evap api.Ingredient
	var e api.Error
	if code := c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: " Evaporated milk ", Unit: api.G}, &evap); code != 201 ||
		evap.Name != "Evaporated milk" || evap.Products != 0 || evap.UnitCostFils != nil {
		t.Fatalf("an ingredient: %d %+v", code, evap)
	}
	if code := c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "evaporated MILK", Unit: api.G}, &e); code != 409 {
		t.Errorf("the same name twice: %d", code)
	}
	if code := c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "Milk", Unit: "cup"}, &e); code != 400 {
		t.Errorf("a unit that isn't one: %d", code)
	}

	saveProduct := func(id uuid.UUID, in api.ProductInput) (api.Product, int) {
		var p api.Product
		code := c.do("PUT", "/v1/admin/products/"+id.String(), in, &p)
		return p, code
	}
	p, code := saveProduct(carton, api.ProductInput{Name: "Rainbow evaporated milk, 48 x 170 g", Area: api.Counter,
		IngredientId: &evap.Id, PackAmount: ptr(8160.0)})
	if code != 200 || p.IngredientId == nil || *p.IngredientId != evap.Id || p.PackAmount == nil || *p.PackAmount != 8160 {
		t.Fatalf("a product's ingredient and pack: %d %+v", code, p)
	}

	price := func() (float64, string) {
		t.Helper()
		var list []api.Ingredient
		c.do("GET", "/v1/admin/ingredients", nil, &list)
		for _, x := range list {
			if x.Id == evap.Id && x.UnitCostFils != nil {
				return *x.UnitCostFils, *x.PricedOn
			}
		}
		return 0, ""
	}
	// 3 cartons of 8,160 g for 210 + 110: fils per gram.
	if got, on := price(); math.Abs(got-32000.0/(3*8160)) > 1e-9 || on != "2026-11-09" {
		t.Errorf("price per gram %v on %s, want %v", got, on, 32000.0/(3*8160))
	}
	// Another brand, another pack, the same ingredient; and only the latest
	// three lines count.
	tins := buy("2026-11-10", "Nido evaporated milk, 12 x 400 g", 2, 6000).Lines[0].ProductId
	saveProduct(tins, api.ProductInput{Name: "Nido evaporated milk, 12 x 400 g", Area: api.Counter, IngredientId: &evap.Id,
		PackAmount: ptr(4800.0)})
	buy("2026-11-16", "Rainbow evaporated milk, 48 x 170 g", 1, 12000)
	want := (11000.0 + 12000 + 12000) / (8160.0 + 2*4800 + 8160)
	if got, on := price(); math.Abs(got-want) > 1e-9 || on != "2026-11-16" {
		t.Errorf("price per gram %v on %s, want %v", got, on, want)
	}

	// The unit stays while products have pack sizes in it.
	if code := c.do("PUT", "/v1/admin/ingredients/"+evap.Id.String(), api.IngredientInput{Name: "Evaporated milk", Unit: api.Ml}, &e); code != 409 {
		t.Errorf("a new unit under pack sizes: %d", code)
	}
	var renamed api.Ingredient
	if code := c.do("PUT", "/v1/admin/ingredients/"+evap.Id.String(), api.IngredientInput{Name: "Evap. milk", Unit: api.G}, &renamed); code != 200 ||
		renamed.Name != "Evap. milk" || renamed.Products != 2 {
		t.Errorf("rename: %d %+v", code, renamed)
	}
	if _, code := saveProduct(tins, api.ProductInput{Name: "RAINBOW evaporated milk, 48 x 170 g", Area: api.Counter}); code != 409 {
		t.Errorf("a product named as another: %d", code)
	}
	if _, code := saveProduct(tins, api.ProductInput{Name: "Nido", Area: api.Counter, IngredientId: &evap.Id, PackAmount: ptr(0.0)}); code != 400 {
		t.Errorf("a pack of nothing: %d", code)
	}
	// A pack size means nothing without an ingredient.
	if p, code := saveProduct(tins, api.ProductInput{Name: "Nido evaporated milk, 12 x 400 g", Area: api.Counter, PackAmount: ptr(4800.0)}); code != 200 ||
		p.IngredientId != nil || p.PackAmount != nil {
		t.Errorf("a pack without an ingredient: %d %+v", code, p)
	}

	// Deleting an ingredient leaves its products, belonging to none.
	if code := c.do("DELETE", "/v1/admin/ingredients/"+evap.Id.String(), nil, nil); code != 204 {
		t.Errorf("delete: %d", code)
	}
	var products []api.Product
	c.do("GET", "/v1/admin/products", nil, &products)
	for _, p := range products {
		if p.IngredientId != nil || p.PackAmount != nil {
			t.Errorf("after deleting its ingredient: %+v", p)
		}
	}

	// Another organization can't touch them.
	if err := otherOrg(c.pool); err != nil {
		t.Fatal(err)
	}
	o := newClient(t, c.ts)
	o.do("POST", "/v1/admin/session", api.SignIn{Email: "other@example.com", Password: ownerPassword}, nil)
	if code := o.do("PUT", "/v1/admin/products/"+carton.String(), api.ProductInput{Name: "Mine", Area: api.Other}, nil); code != 404 {
		t.Errorf("another organization's product: %d", code)
	}
	var mine api.Ingredient
	o.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "Sugar", Unit: api.G}, &mine)
	if _, code := saveProduct(carton, api.ProductInput{Name: "Rainbow evaporated milk, 48 x 170 g", Area: api.Counter,
		IngredientId: &mine.Id, PackAmount: ptr(1.0)}); code != 404 {
		t.Errorf("another organization's ingredient: %d", code)
	}
}
