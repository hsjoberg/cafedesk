package server_test

import (
	"context"
	"testing"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

// A recipe's cost follows its ingredients' prices into the item's cost
// history; options adjust it, batches are portioned, and a missing price
// says so rather than guessing.
func TestRecipes(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop := me.Shops[0].Id

	var sugarGroup api.ModifierGroup
	c.do("POST", "/v1/admin/modifier-groups", api.ModifierGroupInput{Name: "Sugar", Modifiers: []api.ModifierInput{
		{Name: "Normal sugar", IsDefault: true}, {Name: "No sugar"}, {Name: "Extra sugar"}}}, &sugarGroup)
	var tea api.Category
	c.do("POST", "/v1/admin/categories", api.CategoryInput{Name: "Tea", Color: "#5F8A3A"}, &tea)
	var karak, lemon api.Item
	c.do("POST", "/v1/admin/items", api.ItemInput{CategoryId: tea.Id, Name: "Karak", PriceFils: 100,
		ModifierGroupIds: []uuid.UUID{sugarGroup.Id}}, &karak)
	c.do("POST", "/v1/admin/items", api.ItemInput{CategoryId: tea.Id, Name: "Lemon tea", PriceFils: 150,
		ModifierGroupIds: []uuid.UUID{}}, &lemon)

	ing := map[string]api.Ingredient{}
	for _, x := range []struct {
		name string
		unit api.Unit
	}{{"Evaporated milk", api.G}, {"Tea dust", api.G}, {"Sugar", api.G}, {"Paper cup", api.Piece}, {"Cardamom", api.G}} {
		var i api.Ingredient
		c.do("POST", "/v1/admin/ingredients", api.IngredientInput{Name: x.name, Unit: x.unit}, &i)
		ing[x.name] = i
	}
	buy := func(ref, product, ingredient string, pack float64, price int64) {
		t.Helper()
		id := ing[ingredient].Id
		if code := c.do("POST", "/v1/admin/purchases", api.PurchaseInput{ShopId: shop, Date: "2026-11-10", Reference: ptr(ref),
			Lines: []api.PurchaseLineInput{{ProductName: ptr(product), Area: api.Counter, Quantity: 1, UnitPriceFils: price,
				TotalFils: price, IngredientId: &id, PackAmount: &pack}}}, nil); code != 201 {
			t.Fatalf("buying %s: %d", product, code)
		}
	}
	buy("1", "Evaporated milk, 48 x 170 g", "Evaporated milk", 8160, 10500)
	buy("2", "Tea dust, 1 kg", "Tea dust", 1000, 2800)
	buy("3", "Sugar, 10 kg", "Sugar", 10000, 4500)
	buy("4", "Paper cups x 1000", "Paper cup", 1000, 9000)

	line := func(name string, amount float64) api.RecipeLine {
		id := ing[name].Id
		return api.RecipeLine{IngredientId: &id, Amount: amount}
	}
	karakRecipe := []api.RecipeLine{line("Evaporated milk", 40), line("Tea dust", 3), line("Sugar", 10), line("Paper cup", 1)}
	var r api.ItemRecipe
	// 40 g at 10500/8160, 3 g at 2.8, 10 g at 0.45, a cup at 9: 73.37 fils.
	if code := c.do("PUT", "/v1/admin/items/"+karak.Id.String()+"/recipe", api.RecipeInput{Lines: karakRecipe}, &r); code != 200 ||
		r.CostFils == nil || *r.CostFils != 73 || len(r.Missing) != 0 || len(r.Lines) != 4 {
		t.Fatalf("karak's recipe: %d %+v", code, r)
	}
	itemCost := func(id uuid.UUID) (*int64, bool) {
		var cat api.Catalog
		c.do("GET", "/v1/admin/catalog", nil, &cat)
		for _, it := range cat.Items {
			if it.Id == id {
				return it.CostFils, it.CostFromRecipe
			}
		}
		return nil, false
	}
	if cost, fromRecipe := itemCost(karak.Id); cost == nil || *cost != 73 || !fromRecipe {
		t.Errorf("karak on the menu: %v %v", cost, fromRecipe)
	}
	// A cost typed on the menu doesn't override the recipe's.
	var saved api.Item
	c.do("PUT", "/v1/admin/items/"+karak.Id.String(), api.ItemInput{CategoryId: tea.Id, Name: "Karak", PriceFils: 100,
		ModifierGroupIds: []uuid.UUID{sugarGroup.Id}, CostFils: ptr(int64(999))}, &saved)
	if saved.CostFils == nil || *saved.CostFils != 73 || !saved.CostFromRecipe {
		t.Errorf("a typed cost over a recipe: %+v", saved)
	}

	// Options adjust: no sugar saves 4.5 fils, extra adds 2.25.
	mods := sugarGroup.Modifiers
	var mr api.ModifierRecipe
	if c.do("PUT", "/v1/admin/modifiers/"+mods[1].Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{line("Sugar", -10)}}, &mr); mr.CostFils == nil || *mr.CostFils != -5 {
		t.Errorf("no sugar: %+v", mr)
	}
	var mr2 api.ModifierRecipe
	if c.do("PUT", "/v1/admin/modifiers/"+mods[2].Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{line("Sugar", 5)}}, &mr2); mr2.CostFils == nil || *mr2.CostFils != 2 {
		t.Errorf("extra sugar: %+v", mr2)
	}

	// Milk dearer: karak's cost follows, from now; its history keeps the old.
	buy("5", "Evaporated milk, 48 x 170 g", "Evaporated milk", 8160, 12000)
	// 40 g at (10500 + 12000) / 16320, the rest as before: 77.05 fils.
	if cost, _ := itemCost(karak.Id); cost == nil || *cost != 77 {
		t.Errorf("karak after milk went up: %v", cost)
	}
	var history int
	c.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM item_costs WHERE item_id = $1", karak.Id).Scan(&history)
	if history != 2 {
		t.Errorf("karak's cost history: %d rows", history)
	}

	// A batch, in portions: syrup from 1 kg of sugar makes 10.
	var syrup api.Batch
	if code := c.do("POST", "/v1/admin/batches", api.BatchInput{Name: "Sugar syrup", YieldPortions: 10,
		Lines: []api.RecipeLine{line("Sugar", 1000)}}, &syrup); code != 201 || syrup.PortionCostFils == nil || *syrup.PortionCostFils != 45 {
		t.Fatalf("a batch: %d %+v", code, syrup)
	}
	var lr api.ItemRecipe
	if c.do("PUT", "/v1/admin/items/"+lemon.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{
		{BatchId: &syrup.Id, Amount: 2}, line("Paper cup", 1)}}, &lr); lr.CostFils == nil || *lr.CostFils != 99 {
		t.Errorf("lemon tea from the batch: %+v", lr)
	}
	// Twice the portions per batch, half the cost per portion, and lemon tea follows.
	c.do("PUT", "/v1/admin/batches/"+syrup.Id.String(), api.BatchInput{Name: "Sugar syrup", YieldPortions: 20,
		Lines: []api.RecipeLine{line("Sugar", 1000)}}, &syrup)
	if cost, _ := itemCost(lemon.Id); cost == nil || *cost != 54 {
		t.Errorf("lemon tea after the batch changed: %v", cost)
	}

	// No price for cardamom: no cost, and why.
	var kr api.ItemRecipe
	if c.do("PUT", "/v1/admin/items/"+karak.Id.String()+"/recipe", api.RecipeInput{Lines: append(karakRecipe, line("Cardamom", 0.5))}, &kr); kr.CostFils != nil ||
		len(kr.Missing) != 1 || kr.Missing[0] != "Cardamom" {
		t.Errorf("a missing price: %+v", kr)
	}
	if cost, _ := itemCost(karak.Id); cost != nil {
		t.Errorf("karak's cost with a price missing: %d", *cost)
	}

	// What isn't a recipe.
	var e api.Error
	sugarID := ing["Sugar"].Id
	for name, in := range map[string]api.RecipeInput{
		"below nothing": {Lines: []api.RecipeLine{line("Sugar", -1)}},
		"nothing":       {Lines: []api.RecipeLine{line("Sugar", 0)}},
		"both":          {Lines: []api.RecipeLine{{IngredientId: &sugarID, BatchId: &syrup.Id, Amount: 1}}},
		"neither":       {Lines: []api.RecipeLine{{Amount: 1}}},
		"4 decimals":    {Lines: []api.RecipeLine{line("Sugar", 1.2345)}},
	} {
		if code := c.do("PUT", "/v1/admin/items/"+lemon.Id.String()+"/recipe", in, &e); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code := c.do("POST", "/v1/admin/batches", api.BatchInput{Name: "Nested", YieldPortions: 1,
		Lines: []api.RecipeLine{{BatchId: &syrup.Id, Amount: 1}}}, &e); code != 400 {
		t.Errorf("a batch in a batch: %d", code)
	}
	if code := c.do("POST", "/v1/admin/batches", api.BatchInput{Name: "sugar SYRUP", YieldPortions: 1, Lines: []api.RecipeLine{}}, &e); code != 409 {
		t.Errorf("a batch's name twice: %d", code)
	}
	// In use: not deleted.
	if code := c.do("DELETE", "/v1/admin/batches/"+syrup.Id.String(), nil, &e); code != 409 {
		t.Errorf("deleting a batch in use: %d", code)
	}
	if code := c.do("DELETE", "/v1/admin/ingredients/"+ing["Paper cup"].Id.String(), nil, &e); code != 409 {
		t.Errorf("deleting an ingredient in use: %d", code)
	}

	// No recipe: the cost is typed again.
	c.do("PUT", "/v1/admin/items/"+lemon.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{}}, nil)
	var typed api.Item
	c.do("PUT", "/v1/admin/items/"+lemon.Id.String(), api.ItemInput{CategoryId: tea.Id, Name: "Lemon tea", PriceFils: 150,
		ModifierGroupIds: []uuid.UUID{}, CostFils: ptr(int64(50))}, &typed)
	saved = typed
	if saved.CostFromRecipe || saved.CostFils == nil || *saved.CostFils != 50 {
		t.Errorf("back to a typed cost: %+v", saved)
	}
	if code := c.do("DELETE", "/v1/admin/batches/"+syrup.Id.String(), nil, nil); code != 204 {
		t.Errorf("deleting a batch no longer used: %d", code)
	}

	var all api.Recipes
	c.do("GET", "/v1/admin/recipes", nil, &all)
	if len(all.Items) != 1 || len(all.Modifiers) != 2 || len(all.Batches) != 0 {
		t.Errorf("all recipes: %d items, %d options, %d batches", len(all.Items), len(all.Modifiers), len(all.Batches))
	}

	// Another organization can't touch them.
	if err := otherOrg(c.pool); err != nil {
		t.Fatal(err)
	}
	o := newClient(t, c.ts)
	o.do("POST", "/v1/admin/session", api.SignIn{Email: "other@example.com", Password: ownerPassword}, nil)
	if code := o.do("PUT", "/v1/admin/items/"+karak.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{}}, nil); code != 404 {
		t.Errorf("another organization's item: %d", code)
	}
	var mine api.Item
	var cat api.Category
	o.do("POST", "/v1/admin/categories", api.CategoryInput{Name: "Mine", Color: "#5F8A3A"}, &cat)
	o.do("POST", "/v1/admin/items", api.ItemInput{CategoryId: cat.Id, Name: "Mine", PriceFils: 1, ModifierGroupIds: []uuid.UUID{}}, &mine)
	if code := o.do("PUT", "/v1/admin/items/"+mine.Id.String()+"/recipe", api.RecipeInput{Lines: []api.RecipeLine{line("Sugar", 1)}}, nil); code != 404 {
		t.Errorf("another organization's ingredient: %d", code)
	}
}
