package server_test

import (
	"math"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

// What sales used, by the recipes, against what was bought: items, batches
// broken into ingredients, options taking away; what's left out, said.
func TestUsage(t *testing.T) {
	c, _, _ := newServer(t)
	shop, token, manager, _ := tillSetup(t, c)
	till := newClient(t, c.ts)

	var cat api.Catalog
	c.do("GET", "/v1/admin/catalog", nil, &cat)
	karak := cat.Items[0]
	var group api.ModifierGroup
	c.do("POST", "/v1/admin/modifier-groups", api.ModifierGroupInput{Name: "Sugar", Modifiers: []api.ModifierInput{
		{Name: "Normal sugar", IsDefault: true}, {Name: "No sugar"}}}, &group)
	noSugar := group.Modifiers[1]
	var lemon, biryani api.Item
	c.do("POST", "/v1/admin/items", api.ItemInput{CategoryId: karak.CategoryId, Name: "Lemon tea", PriceFils: 150,
		ModifierGroupIds: []uuid.UUID{}}, &lemon)
	c.do("POST", "/v1/admin/items", api.ItemInput{CategoryId: karak.CategoryId, Name: "Biryani", PriceFils: 1400,
		ModifierGroupIds: []uuid.UUID{}}, &biryani)

	ing := map[string]uuid.UUID{}
	for _, x := range []struct {
		name string
		unit api.Unit
	}{{"Evaporated milk", api.G}, {"Sugar", api.G}, {"Paper cup", api.Piece}} {
		var i api.Ingredient
		c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: x.name, Unit: x.unit}, &i)
		ing[x.name] = i.Id
	}
	line := func(name string, amount float64) api.RecipeLine {
		id := ing[name]
		return api.RecipeLine{IngredientId: &id, Amount: amount}
	}
	pl := func(product, ingredient string, pack float64, price int64) api.PurchaseLineInput {
		l := api.PurchaseLineInput{ProductName: ptr(product), Area: api.Counter, Quantity: 1, UnitPriceFils: price, TotalFils: price}
		if ingredient != "" {
			id := ing[ingredient]
			l.IngredientId, l.PackAmount = &id, &pack
		}
		return l
	}
	if code := c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop.Id, Date: "2026-11-12", Lines: []api.PurchaseLineInput{
		pl("Evaporated milk, 48 x 170 g", "Evaporated milk", 8160, 10500),
		pl("Sugar, 10 kg", "Sugar", 10000, 4500),
		pl("Paper cups x 1000", "Paper cup", 1000, 9000),
		pl("Dish soap, 5 L", "", 0, 2500),
	}}, nil); code != 201 {
		t.Fatalf("purchase: %d", code)
	}
	// Outside the dates: not counted.
	c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop.Id, Date: "2026-10-01", Lines: []api.PurchaseLineInput{
		pl("Sugar, 10 kg", "Sugar", 10000, 4500)}}, nil)

	var syrup api.Batch
	c.do("POST", "/v1/admin/batches", api.BatchInput{Name: "Sugar syrup", YieldPortions: 10, Lines: []api.RecipeLine{line("Sugar", 1000)}}, &syrup)
	c.do("PUT", "/v1/admin/items/"+karak.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{
		line("Evaporated milk", 40), line("Sugar", 10), line("Paper cup", 1)}}, nil)
	c.do("PUT", "/v1/admin/items/"+lemon.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{
		{BatchId: &syrup.Id, Amount: 2}, line("Paper cup", 1)}}, nil)
	c.do("PUT", "/v1/admin/modifiers/"+noSugar.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{line("Sugar", -10)}}, nil)

	// Sold on the 12th: 2 karak, 1 karak without sugar, 2 lemon teas, a
	// biryani (no recipe); a voided karak doesn't count.
	day := uuid.New()
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	ol := func(item api.Item, qty int, mods ...api.OrderLineModifierEvent) api.OrderLineEvent {
		if mods == nil {
			mods = []api.OrderLineModifierEvent{}
		}
		return api.OrderLineEvent{Id: uuid.New(), ItemId: item.Id.String(), Name: item.Name, Qty: qty, UnitPriceFils: item.PriceFils,
			LineTotalFils: int64(qty) * item.PriceFils, Modifiers: mods}
	}
	order := func(n int, lines ...api.OrderLineEvent) api.OrderEvent {
		var total int64
		for _, l := range lines {
			total += l.LineTotalFils
		}
		return api.OrderEvent{Id: uuid.New(), BusinessDayId: day, Number: n, Code: "T1-00" + itoa(int64(n)), CreatedAt: at,
			StaffId: manager.Id, StaffName: "Ravi", TotalFils: total, PaymentMethod: api.Cash, TenderedFils: total, Lines: lines}
	}
	o1 := order(1, ol(karak, 2), ol(karak, 1, api.OrderLineModifierEvent{GroupId: group.Id.String(), GroupName: "Sugar",
		ModifierId: noSugar.Id.String(), Name: "No sugar"}), ol(lemon, 2), ol(biryani, 1))
	o2 := order(2, ol(karak, 5))
	var res api.EventResult
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 1, Type: api.BusinessDay, BusinessDay: &api.BusinessDayEvent{Id: day, Date: "2026-11-12", OpenedAt: at,
			OpenedBy: manager.Id, OpenedByName: "Ravi"}},
		{Seq: 2, Type: api.Order, Order: &o1},
		{Seq: 3, Type: api.Order, Order: &o2},
		{Seq: 4, Type: api.OrderVoid, OrderVoid: &api.OrderVoidEvent{OrderId: o2.Id, VoidedAt: at, VoidedBy: manager.Id,
			VoidedByName: "Ravi", Reason: "Test"}},
	}}, &res, bearer(token)...)
	if len(res.Accepted) != 4 {
		t.Fatalf("sales: %+v", res)
	}

	var u api.Usage
	if code := c.do("GET", "/v1/admin/reports/usage?from=2026-11-01&to=2026-11-30", nil, &u); code != 200 {
		t.Fatalf("usage: %d", code)
	}
	want := map[string][2]float64{
		"Evaporated milk": {8160, 3 * 40},
		// 3 karak × 10, less 10 for no sugar, and 4 portions of syrup at 100 g each.
		"Sugar":     {10000, 30 - 10 + 400},
		"Paper cup": {1000, 3 + 2},
	}
	if len(u.Ingredients) != 3 {
		t.Fatalf("ingredients: %+v", u.Ingredients)
	}
	for _, x := range u.Ingredients {
		w := want[x.Name]
		if x.Bought != w[0] || x.Used != w[1] {
			t.Errorf("%s: bought %v, used %v; want %v", x.Name, x.Bought, x.Used, w)
		}
	}
	// 120 g of milk at 10500/8160, 420 g of sugar at 0.45, 5 cups at 9.
	usedFils := int64(math.Round(120*10500.0/8160 + 420*0.45 + 5*9))
	if u.BoughtFils != 24000 || u.UsedFils != usedFils || u.UnsortedFils != 2500 || u.UnrecipedFils != 1400 {
		t.Errorf("totals: bought %d, used %d (want %d), unsorted %d, without a recipe %d",
			u.BoughtFils, u.UsedFils, usedFils, u.UnsortedFils, u.UnrecipedFils)
	}
	// The largest difference in money first: milk, 8,040 g over, at 1.29 fils a gram.
	if u.Ingredients[0].Name != "Evaporated milk" {
		t.Errorf("first: %s", u.Ingredients[0].Name)
	}
	if code := c.do("GET", "/v1/admin/reports/usage?from=2026-11-30&to=2026-11-01", nil, nil); code != 400 {
		t.Errorf("dates backwards: %d", code)
	}
}
