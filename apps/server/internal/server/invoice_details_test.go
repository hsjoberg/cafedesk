package server_test

import (
	"testing"

	"cafedesk/apps/server/internal/api"
)

// A wholesale invoice as they come: prices before VAT, VAT per line, the
// wholesaler's item codes. And a say over the overdue warning.
func TestInvoiceDetails(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop := me.Shops[0].Id

	yes := true
	inv := api.PurchaseInput{ShopId: shop, Date: "2026-11-10", SupplierName: ptr("Wholesale"), Reference: ptr("A-1"),
		PricesBeforeVat: &yes, Lines: []api.PurchaseLineInput{
			{ProductCode: ptr(" 1002683 "), ProductName: ptr("Coca Cola Zero Can 24x330ml"), Area: api.Counter,
				Quantity: 1, UnitPriceFils: 3900, TotalFils: 4095, VatFils: ptr(int64(195))},
			{ProductCode: ptr("1039702"), ProductName: ptr("Uncle J's Gold Long Grain Rice 35kg"), Area: api.Kitchen,
				Quantity: 1, UnitPriceFils: 11700, TotalFils: 12285, VatFils: ptr(int64(585))},
		}}
	var p api.Purchase
	if code := c.do("POST", "/v1/admin/purchases", inv, &p); code != 201 || p.TotalFils != 16380 || p.VatFils != 780 ||
		!p.PricesBeforeVat || p.Lines[0].VatFils != 195 || p.Lines[0].ProductCode == nil || *p.Lines[0].ProductCode != "1002683" {
		t.Fatalf("an invoice with VAT and codes: %d %+v", code, p)
	}
	coke := p.Lines[0].ProductId

	// Next week the code names the product, whatever the name says.
	var p2 api.Purchase
	c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop, Date: "2026-11-17", SupplierName: ptr("Wholesale"),
		Reference: ptr("A-2"), PricesBeforeVat: &yes, Lines: []api.PurchaseLineInput{
			{ProductCode: ptr("1002683"), ProductName: ptr("COKE ZERO 24"), Area: api.Counter, Quantity: 2,
				UnitPriceFils: 4000, TotalFils: 8400, VatFils: ptr(int64(400))},
		}}, &p2)
	if len(p2.Lines) != 1 || p2.Lines[0].ProductId != coke || p2.Lines[0].ProductName != "Coca Cola Zero Can 24x330ml" {
		t.Errorf("by code: %+v", p2.Lines)
	}
	var products []api.Product
	c.do("GET", "/v1/admin/products", nil, &products)
	for _, x := range products {
		if x.Id == coke && (x.LastCostEachFils == nil || *x.LastCostEachFils != 4200 || x.LastUnitPriceFils == nil ||
			*x.LastUnitPriceFils != 4000) {
			t.Errorf("what one cost last, VAT in: %+v", x)
		}
	}

	// A product known by name learns its code from an invoice.
	c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop, Date: "2026-11-17", Lines: []api.PurchaseLineInput{
		{ProductName: ptr("Fanta Orange (F) 6x2.25ltr"), Area: api.Counter, Quantity: 1, TotalFils: 3990}}}, nil)
	c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop, Date: "2026-11-17", Lines: []api.PurchaseLineInput{
		{ProductCode: ptr("1004380"), ProductName: ptr("fanta orange (f) 6x2.25ltr"), Area: api.Counter, Quantity: 1, TotalFils: 3990}}}, nil)
	c.do("GET", "/v1/admin/products", nil, &products)
	fanta := 0
	for _, x := range products {
		if x.Name == "Fanta Orange (F) 6x2.25ltr" {
			fanta++
			if x.Code == nil || *x.Code != "1004380" {
				t.Errorf("a code learnt: %+v", x)
			}
		}
	}
	if fanta != 1 {
		t.Errorf("%d Fantas", fanta)
	}

	// VAT is part of a line's amount, never more.
	var e api.Error
	bad := inv
	bad.Reference = ptr("A-3")
	bad.Lines = []api.PurchaseLineInput{{ProductName: ptr("X"), Area: api.Other, Quantity: 1, TotalFils: 100, VatFils: ptr(int64(101))}}
	if code := c.do("POST", "/v1/admin/purchases", bad, &e); code != 400 {
		t.Errorf("VAT over the amount: %d", code)
	}
	// Without prices before VAT, a line's VAT is none.
	plain := inv
	plain.Reference, plain.PricesBeforeVat = ptr("A-4"), nil
	plain.Lines = []api.PurchaseLineInput{{ProductName: ptr("Y"), Area: api.Other, Quantity: 1, TotalFils: 100, VatFils: ptr(int64(5))}}
	var p3 api.Purchase
	if c.do("POST", "/v1/admin/purchases", plain, &p3); p3.VatFils != 0 || p3.PricesBeforeVat {
		t.Errorf("VAT on prices that include it: %+v", p3)
	}

	// The supplier: quiet until a date, not watched; an invoice ends the quiet.
	supplier := p.Supplier.Id
	var s api.Supplier
	if code := c.do("PUT", "/v1/admin/suppliers/"+supplier.String(), api.SupplierInput{Watched: true, QuietUntil: ptr("2026-11-30")}, &s); code != 200 ||
		s.QuietUntil == nil || *s.QuietUntil != "2026-11-30" || !s.Watched {
		t.Fatalf("quiet: %d %+v", code, s)
	}
	if code := c.do("PUT", "/v1/admin/suppliers/"+supplier.String(), api.SupplierInput{Watched: true, QuietUntil: ptr("soon")}, &e); code != 400 {
		t.Errorf("a quiet that isn't a date: %d", code)
	}
	inv.Reference, inv.Date = ptr("A-5"), "2026-11-24"
	c.do("POST", "/v1/admin/purchases", inv, nil)
	var suppliers []api.Supplier
	c.do("GET", "/v1/admin/suppliers", nil, &suppliers)
	if len(suppliers) != 1 || suppliers[0].QuietUntil != nil {
		t.Errorf("after its invoice: %+v", suppliers)
	}
	var off api.Supplier
	c.do("PUT", "/v1/admin/suppliers/"+supplier.String(), api.SupplierInput{Watched: false}, &off)
	if off.Watched || off.QuietUntil != nil {
		t.Errorf("not watched: %+v", off)
	}

	if err := otherOrg(c.pool); err != nil {
		t.Fatal(err)
	}
	o := newClient(t, c.ts)
	o.do("POST", "/v1/admin/session", api.SignIn{Email: "other@example.com", Password: ownerPassword}, nil)
	if code := o.do("PUT", "/v1/admin/suppliers/"+supplier.String(), api.SupplierInput{Watched: true}, nil); code != 404 {
		t.Errorf("another organization's supplier: %d", code)
	}
}
