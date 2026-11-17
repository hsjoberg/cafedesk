package server_test

import (
	"context"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

func TestPurchases(t *testing.T) {
	c, _, _ := newServer(t)
	shop, token, manager, _ := tillSetup(t, c)
	till := newClient(t, c.ts)
	ctx := context.Background()
	date := "2026-11-12"

	week := api.PurchaseInput{ShopId: shop.Id, Date: date, SupplierName: ptr("  Al Madina Wholesale "), Reference: ptr("INV-0042"),
		Lines: []api.PurchaseLineInput{
			{ProductName: ptr("Rainbow evaporated milk, 48 x 170 g"), Area: api.Counter, Quantity: 2, UnitPriceFils: 10500, TotalFils: 21000},
			{ProductName: ptr("Basmati rice, 5 kg"), Area: api.Kitchen, Quantity: 1.5, UnitPriceFils: 4000, TotalFils: 6000},
			{ProductName: ptr("Paper cups, 8 oz x 1000"), Area: api.Packaging, Quantity: 1, UnitPriceFils: 9000, TotalFils: 9000},
		}}
	var p api.Purchase
	if code := c.do("POST", "/v1/admin/purchases", week, &p); code != 201 || p.TotalFils != 36000 || p.Supplier == nil ||
		p.Supplier.Name != "Al Madina Wholesale" || p.Reference != "INV-0042" || len(p.Lines) != 3 || p.Lines[1].Quantity != 1.5 ||
		p.CreatedByName != "Owner" {
		t.Fatalf("a purchase: %d %+v", code, p)
	}
	rice := p.Lines[1].ProductId

	// The supplier and products are remembered, whatever the case of their
	// names; a line's area becomes its product's.
	oneOff := api.PurchaseInput{ShopId: shop.Id, Date: "2026-11-13", SupplierName: ptr("al madina wholesale"),
		Lines: []api.PurchaseLineInput{
			{ProductId: &rice, Area: api.Other, Quantity: 1, UnitPriceFils: 4200, TotalFils: 4200},
			{ProductName: ptr("BASMATI RICE, 5 KG"), Area: api.Kitchen, Quantity: 1, UnitPriceFils: 4100, TotalFils: 4100},
		}}
	var p2 api.Purchase
	if code := c.do("POST", "/v1/admin/purchases", oneOff, &p2); code != 201 || p2.Supplier.Id != p.Supplier.Id ||
		p2.Lines[1].ProductId != rice {
		t.Fatalf("a second purchase: %d %+v", code, p2)
	}
	var suppliers []api.Supplier
	var products []api.Product
	c.do("GET", "/v1/admin/suppliers", nil, &suppliers)
	c.do("GET", "/v1/admin/products", nil, &products)
	if len(suppliers) != 1 || len(products) != 3 {
		t.Fatalf("%d suppliers, %d products", len(suppliers), len(products))
	}
	for _, pr := range products {
		if pr.Id == rice && (pr.Area != api.Kitchen || pr.LastUnitPriceFils == nil || *pr.LastUnitPriceFils != 4100 ||
			pr.LastPurchasedOn == nil || *pr.LastPurchasedOn != "2026-11-13") {
			t.Errorf("rice, as last bought: %+v", pr)
		}
	}

	// What isn't a purchase.
	var e api.Error
	bad := func(name string, in api.PurchaseInput, want int) {
		t.Helper()
		if code := c.do("POST", "/v1/admin/purchases", in, &e); code != want {
			t.Errorf("%s: %d %+v", name, code, e)
		}
	}
	line := func(l api.PurchaseLineInput) api.PurchaseInput {
		return api.PurchaseInput{ShopId: shop.Id, Date: date, Lines: []api.PurchaseLineInput{l}}
	}
	ok := api.PurchaseLineInput{ProductName: ptr("Sugar, 10 kg"), Area: api.Counter, Quantity: 1, UnitPriceFils: 4500, TotalFils: 4500}
	bad("no lines", api.PurchaseInput{ShopId: shop.Id, Date: date, Lines: []api.PurchaseLineInput{}}, 400)
	bad("no date", api.PurchaseInput{ShopId: shop.Id, Lines: []api.PurchaseLineInput{ok}}, 400)
	for name, l := range map[string]api.PurchaseLineInput{
		"no product":    {Area: api.Counter, Quantity: 1, TotalFils: 1},
		"no quantity":   {ProductName: ptr("Sugar"), Area: api.Counter, Quantity: 0, TotalFils: 1},
		"4 decimals":    {ProductName: ptr("Sugar"), Area: api.Counter, Quantity: 1.2345, TotalFils: 1},
		"no area":       {ProductName: ptr("Sugar"), Area: "pantry", Quantity: 1, TotalFils: 1},
		"below nothing": {ProductName: ptr("Sugar"), Area: api.Counter, Quantity: 1, TotalFils: -1},
	} {
		bad(name, line(l), 400)
	}
	other := uuid.New()
	bad("another shop", api.PurchaseInput{ShopId: other, Date: date, Lines: []api.PurchaseLineInput{ok}}, 404)
	bad("an unknown product", line(api.PurchaseLineInput{ProductId: &other, Area: api.Counter, Quantity: 1}), 404)
	bad("an unknown supplier", api.PurchaseInput{ShopId: shop.Id, Date: date, SupplierId: &other, Lines: []api.PurchaseLineInput{ok}}, 404)

	// Sales by area: a kitchen line and a counter line on the 12th.
	day := uuid.New()
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	order := api.OrderEvent{Id: uuid.New(), BusinessDayId: day, Number: 1, Code: "T1-001", CreatedAt: now, StaffId: manager.Id,
		StaffName: "Ravi", TotalFils: 1500, PaymentMethod: api.Cash, TenderedFils: 1500,
		Lines: []api.OrderLineEvent{
			{Id: uuid.New(), ItemId: "biryani", Name: "Biryani", Qty: 1, UnitPriceFils: 1300, LineTotalFils: 1300, Kitchen: true,
				Modifiers: []api.OrderLineModifierEvent{}},
			{Id: uuid.New(), ItemId: "karak", Name: "Karak", Qty: 2, UnitPriceFils: 100, LineTotalFils: 200,
				Modifiers: []api.OrderLineModifierEvent{}},
		}}
	var res api.EventResult
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 1, Type: api.EventTypeBusinessDay, BusinessDay: &api.BusinessDayEvent{Id: day, Date: date, OpenedAt: now,
			OpenedBy: manager.Id, OpenedByName: "Ravi"}},
		{Seq: 2, Type: api.EventTypeOrder, Order: &order},
		{Seq: 3, Type: api.EventTypeRefund, Refund: &api.RefundEvent{Id: uuid.New(), OrderId: order.Id, BusinessDayId: day, CreatedAt: now,
			StaffId: manager.Id, StaffName: "Ravi", ApprovedBy: manager.Id, ApprovedByName: "Ravi", Reason: "Other",
			Method: api.Cash, TotalFils: 100, Lines: []api.RefundLineEvent{{OrderLineId: order.Lines[1].Id, Qty: 1, AmountFils: 100}}}},
	}}, &res, bearer(token)...)
	if len(res.Accepted) != 3 {
		t.Fatalf("sales: %+v", res)
	}

	var list api.PurchaseList
	if code := c.do("GET", "/v1/admin/purchases?from=2026-11-12&to=2026-11-13", nil, &list); code != 200 {
		t.Fatalf("list: %d", code)
	}
	if len(list.Purchases) != 2 || list.Purchases[0].Id != p2.Id || list.TotalFils != 36000+8300 || list.SalesFils != 1400 {
		t.Errorf("list: %d purchases, %d bought, %d sales", len(list.Purchases), list.TotalFils, list.SalesFils)
	}
	want := map[api.Area][2]int64{api.Counter: {21000, 100}, api.Kitchen: {6000 + 4100, 1300}, api.Packaging: {9000, -1},
		api.Other: {4200, -1}}
	if len(list.Areas) != 4 || list.Areas[0].Area != api.Counter {
		t.Fatalf("areas: %+v", list.Areas)
	}
	for _, a := range list.Areas {
		w := want[a.Area]
		sales := int64(-1)
		if a.SalesFils != nil {
			sales = *a.SalesFils
		}
		if a.BoughtFils != w[0] || sales != w[1] {
			t.Errorf("%s: bought %d, sales %d; want %v", a.Area, a.BoughtFils, sales, w)
		}
	}

	// Edit: the lines are replaced, and who changed it is kept.
	week.Lines = week.Lines[:1]
	week.SupplierName = nil
	var edited api.Purchase
	if code := c.do("PUT", "/v1/admin/purchases/"+p.Id.String(), week, &edited); code != 200 || edited.TotalFils != 21000 ||
		len(edited.Lines) != 1 || edited.Supplier != nil || edited.UpdatedByName == nil || *edited.UpdatedByName != "Owner" {
		t.Errorf("edit: %d %+v", code, edited)
	}
	// Delete: gone from the list, kept in the database.
	if code := c.do("DELETE", "/v1/admin/purchases/"+p2.Id.String(), nil, nil); code != 204 {
		t.Errorf("delete: %d", code)
	}
	if code := c.do("DELETE", "/v1/admin/purchases/"+p2.Id.String(), nil, nil); code != 404 {
		t.Errorf("delete twice: %d", code)
	}
	if code := c.do("PUT", "/v1/admin/purchases/"+p2.Id.String(), oneOff, nil); code != 404 {
		t.Errorf("edit a deleted purchase: %d", code)
	}
	c.do("GET", "/v1/admin/purchases?from=2026-11-12&to=2026-11-13", nil, &list)
	var kept int
	c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM purchases WHERE deleted_at IS NOT NULL AND deleted_by IS NOT NULL").Scan(&kept)
	if len(list.Purchases) != 1 || list.TotalFils != 21000 || kept != 1 {
		t.Errorf("after deleting: %d purchases, %d bought, %d kept", len(list.Purchases), list.TotalFils, kept)
	}

	// Another organization sees none of it, and can't change it.
	if err := otherOrg(c.pool); err != nil {
		t.Fatal(err)
	}
	o := newClient(t, c.ts)
	o.do("POST", "/v1/admin/session", api.SignIn{Username: "other", Password: ownerPassword}, nil)
	o.do("GET", "/v1/admin/products", nil, &products)
	o.do("GET", "/v1/admin/purchases?from=2026-11-12&to=2026-11-13", nil, &list)
	if len(products) != 0 || len(list.Purchases) != 0 {
		t.Errorf("another organization sees %d products, %d purchases", len(products), len(list.Purchases))
	}
	if code := o.do("DELETE", "/v1/admin/purchases/"+p.Id.String(), nil, nil); code != 404 {
		t.Errorf("another organization deletes: %d", code)
	}
	if code := o.do("POST", "/v1/admin/purchases", line(ok), nil); code != 404 {
		t.Errorf("another organization buys for this shop: %d", code)
	}
}
