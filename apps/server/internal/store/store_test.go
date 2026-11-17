package store_test

import (
	"context"
	"errors"
	"testing"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/db"
	"cafedesk/apps/server/internal/store"
	"cafedesk/apps/server/internal/testdb"

	"github.com/google/uuid"
)

// uae is a business in the UAE, in dirhams.
var uae = store.Settings{Currency: "AED", Country: store.CountryUAE, Timezone: "Asia/Dubai"}

func setup(t *testing.T) (*store.Store, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	org, _, _, err := st.Setup(context.Background(), "Test Org", "Test Shop", "Owner", "owner", "hash", uae)
	if err != nil {
		t.Fatal(err)
	}
	return st, org.ID
}

func TestMigrateTwice(t *testing.T) {
	pool := testdb.New(t)
	n, err := db.Migrate(context.Background(), pool)
	if err != nil || n != 0 {
		t.Errorf("second migration applied %d, %v", n, err)
	}
}

func TestSetupOnce(t *testing.T) {
	st, _ := setup(t)
	_, _, _, err := st.Setup(context.Background(), "Other", "Shop", "Someone", "someone", "hash", uae)
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("a second setup: %v", err)
	}
	u, hash, err := st.UserByUsername(context.Background(), "  OWNER ")
	if err != nil || u.Role != "owner" || hash != "hash" {
		t.Errorf("owner %+v %q %v", u, hash, err)
	}
}

func TestDemoMenu(t *testing.T) {
	st, org := setup(t)
	ctx := context.Background()
	if err := st.SeedDemoMenu(ctx, org); err != nil {
		t.Fatal(err)
	}
	c, err := st.Catalog(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Categories) != 4 || len(c.Items) != 37 || len(c.ModifierGroups) != 3 {
		t.Fatalf("%d categories, %d items, %d groups", len(c.Categories), len(c.Items), len(c.ModifierGroups))
	}
	if c.Items[0].Name != "Karak chai" || len(c.Items[0].ModifierGroupIds) != 1 || c.Items[0].Position != 0 {
		t.Errorf("first item %+v", c.Items[0])
	}
}

func TestVersionsAndMoves(t *testing.T) {
	st, org := setup(t)
	ctx := context.Background()
	cat, _ := st.CreateCategory(ctx, org, api.CategoryInput{Name: "Tea", Color: "#5F8A3A"})
	var ids []uuid.UUID
	for _, name := range []string{"A", "B", "C", "D"} {
		it, err := st.CreateItem(ctx, org, api.ItemInput{CategoryId: cat.Id, Name: name, PriceFils: 100,
			ModifierGroupIds: []uuid.UUID{}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.Id)
	}
	before, _ := st.Catalog(ctx, org)

	if err := st.MoveItem(ctx, org, ids[3], 0); err != nil { // D first
		t.Fatal(err)
	}
	if err := st.MoveItem(ctx, org, ids[0], 99); err != nil { // A last
		t.Fatal(err)
	}
	after, _ := st.Catalog(ctx, org)
	var order string
	for _, it := range after.Items {
		order += it.Name
	}
	if order != "DBCA" {
		t.Errorf("order %q, want DBCA", order)
	}
	if after.Version != before.Version+2 {
		t.Errorf("version %d after two moves from %d", after.Version, before.Version)
	}

	// Deleting closes the gap, and archives rather than deletes.
	if err := st.DeleteItem(ctx, org, ids[1]); err != nil { // B
		t.Fatal(err)
	}
	after, _ = st.Catalog(ctx, org)
	for i, it := range after.Items {
		if it.Position != i {
			t.Errorf("%s at %d, want %d", it.Name, it.Position, i)
		}
	}
	if err := st.DeleteItem(ctx, org, ids[1]); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestDeleteRules(t *testing.T) {
	st, org := setup(t)
	ctx := context.Background()
	g, _ := st.CreateModifierGroup(ctx, org, api.ModifierGroupInput{Name: "Sugar",
		Modifiers: []api.ModifierInput{{Name: "Normal", IsDefault: true}, {Name: "Less"}}})
	cat, _ := st.CreateCategory(ctx, org, api.CategoryInput{Name: "Tea", Color: "#5F8A3A"})
	it, _ := st.CreateItem(ctx, org, api.ItemInput{CategoryId: cat.Id, Name: "Karak", PriceFils: 100,
		ModifierGroupIds: []uuid.UUID{g.Id}})

	var p *store.Problem
	if err := st.DeleteCategory(ctx, org, cat.Id); !errors.Is(err, store.ErrConflict) || !errors.As(err, &p) {
		t.Errorf("deleted a category with items: %v", err)
	}
	if err := st.DeleteModifierGroup(ctx, org, g.Id); err != nil {
		t.Fatal(err)
	}
	c, _ := st.Catalog(ctx, org)
	if len(c.Items[0].ModifierGroupIds) != 0 || len(c.ModifierGroups) != 0 {
		t.Errorf("the deleted group is still on the item: %+v", c.Items[0])
	}
	if _, err := st.CreateItem(ctx, org, api.ItemInput{CategoryId: cat.Id, Name: "X",
		ModifierGroupIds: []uuid.UUID{g.Id}}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an item took a deleted group: %v", err)
	}
	st.DeleteItem(ctx, org, it.Id)
	if err := st.DeleteCategory(ctx, org, cat.Id); err != nil {
		t.Errorf("deleting an empty category: %v", err)
	}
}

func TestModifiersKeepTheirIDs(t *testing.T) {
	st, org := setup(t)
	ctx := context.Background()
	g, _ := st.CreateModifierGroup(ctx, org, api.ModifierGroupInput{Name: "Sugar",
		Modifiers: []api.ModifierInput{{Name: "Normal", IsDefault: true}, {Name: "Less"}, {Name: "None"}}})
	keep, drop := g.Modifiers[1].Id, g.Modifiers[2].Id
	g2, err := st.UpdateModifierGroup(ctx, org, g.Id, api.ModifierGroupInput{Name: "Sugar",
		Modifiers: []api.ModifierInput{{Id: &keep, Name: "Less sugar"}, {Name: "Extra"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(g2.Modifiers) != 2 || g2.Modifiers[0].Id != keep || g2.Modifiers[0].Name != "Less sugar" || g2.Modifiers[1].Id == drop {
		t.Errorf("modifiers %+v", g2.Modifiers)
	}
	c, _ := st.Catalog(ctx, org)
	if len(c.ModifierGroups[0].Modifiers) != 2 {
		t.Errorf("catalog modifiers %+v", c.ModifierGroups[0].Modifiers)
	}
}
