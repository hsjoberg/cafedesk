package server_test

import (
	"testing"

	"cafedesk/apps/server/internal/api"
)

// A new product can be sorted as it is first bought; one already sorted
// stays as it is. An invoice is recorded once; a supplier's usual gap is
// learnt from its invoices.
func TestPurchaseSortingAndInvoices(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop := me.Shops[0].Id

	var rice, sugar api.Ingredient
	c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "Basmati rice", Unit: api.G}, &rice)
	c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: "Sugar", Unit: api.G}, &sugar)
	buy := func(date, supplier, ref string, l api.PurchaseLineInput) (api.Purchase, int, api.Error) {
		var p api.Purchase
		var e api.Error
		in := api.PurchaseInput{ShopId: shop, Date: date, Reference: ptr(ref), Lines: []api.PurchaseLineInput{l}}
		if supplier != "" {
			in.SupplierName = ptr(supplier)
		}
		code := c.do("POST", "/v1/admin/purchases", in, &p)
		if code != 201 {
			c.do("POST", "/v1/admin/purchases", in, &e)
		}
		return p, code, e
	}
	bag := func(ing *api.Ingredient, pack float64) api.PurchaseLineInput {
		l := api.PurchaseLineInput{ProductName: ptr("Basmati rice, 10 kg"), Area: api.Kitchen, Quantity: 1, UnitPriceFils: 6000, TotalFils: 6000}
		if ing != nil {
			l.IngredientId, l.PackAmount = &ing.Id, &pack
		}
		return l
	}

	p, code, _ := buy("2026-11-01", "Wholesale", "INV-1", bag(&rice, 10000))
	if code != 201 {
		t.Fatalf("a purchase sorting its product: %d", code)
	}
	product := p.Lines[0].ProductId
	productIs := func() (*api.Product, bool) {
		var list []api.Product
		c.do("GET", "/v1/admin/products", nil, &list)
		for _, x := range list {
			if x.Id == product {
				return &x, x.IngredientId != nil && *x.IngredientId == rice.Id && x.PackAmount != nil && *x.PackAmount == 10000
			}
		}
		return nil, false
	}
	if x, ok := productIs(); !ok {
		t.Fatalf("sorted as bought: %+v", x)
	}
	// Bought again, naming another ingredient: it stays rice.
	if _, code, _ := buy("2026-11-08", "Wholesale", "INV-2", bag(&sugar, 5)); code != 201 {
		t.Fatalf("buying it again: %d", code)
	}
	if x, ok := productIs(); !ok {
		t.Errorf("re-sorted by a purchase: %+v", x)
	}
	missing := api.Ingredient{Id: shop} // not an ingredient
	if _, code, _ := buy("2026-11-08", "", "", api.PurchaseLineInput{ProductName: ptr("Tea, 1 kg"), Area: api.Counter,
		Quantity: 1, TotalFils: 1, IngredientId: &missing.Id, PackAmount: ptr(1000.0)}); code != 404 {
		t.Errorf("an unknown ingredient: %d", code)
	}
	if _, code, _ := buy("2026-11-08", "", "", api.PurchaseLineInput{ProductName: ptr("Tea, 1 kg"), Area: api.Counter,
		Quantity: 1, TotalFils: 1, IngredientId: &rice.Id, PackAmount: ptr(-1.0)}); code != 400 {
		t.Errorf("a pack below nothing: %d", code)
	}

	// The same invoice twice, whatever its case, is refused; another
	// supplier's may share its number.
	if _, code, e := buy("2026-11-09", "Wholesale", "inv-2", bag(nil, 0)); code != 409 ||
		e.Error.Message != "Invoice inv-2 from Wholesale is already recorded, on 8 Nov 2026." {
		t.Errorf("an invoice twice: %d %q", code, e.Error.Message)
	}
	if _, code, _ := buy("2026-11-09", "Grocer", "INV-2", bag(nil, 0)); code != 201 {
		t.Errorf("another supplier's INV-2: %d", code)
	}
	// Saving the invoice itself again is fine.
	in := api.PurchaseInput{ShopId: shop, Date: "2026-11-01", SupplierName: ptr("Wholesale"), Reference: ptr("INV-1"),
		Lines: []api.PurchaseLineInput{bag(nil, 0)}}
	if code := c.do("PUT", "/v1/admin/purchases/"+p.Id.String(), in, nil); code != 200 {
		t.Errorf("saving an invoice again: %d", code)
	}

	buy("2026-11-15", "Wholesale", "INV-3", bag(nil, 0))
	var suppliers []api.Supplier
	c.do("GET", "/v1/admin/suppliers", nil, &suppliers)
	for _, s := range suppliers {
		switch s.Name {
		case "Wholesale":
			if s.UsualDays == nil || *s.UsualDays != 7 || s.LastPurchasedOn == nil || *s.LastPurchasedOn != "2026-11-15" {
				t.Errorf("the wholesaler's gap: %+v", s)
			}
		case "Grocer":
			if s.UsualDays != nil {
				t.Errorf("a gap from one purchase: %d", *s.UsualDays)
			}
		}
	}
}
