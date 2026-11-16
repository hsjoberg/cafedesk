package store

import (
	"context"
	"fmt"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

type demoPart struct {
	name   string // an ingredient, or a batch
	amount float64
}

// SeedDemoRecipes gives the demo's teas and some meals recipes, the meals
// from the chef's batches, and the sugar and extras options adjustments.
// It needs the demo's ingredients (SeedDemoIngredients). The other items
// keep their typed costs.
func (s *Store) SeedDemoRecipes(ctx context.Context, orgID uuid.UUID) error {
	ings, err := s.ListIngredients(ctx, orgID)
	if err != nil {
		return err
	}
	ingID := map[string]uuid.UUID{}
	for _, i := range ings {
		ingID[i.Name] = i.Id
	}
	batchID := map[string]uuid.UUID{}
	lines := func(parts []demoPart) ([]api.RecipeLine, error) {
		out := []api.RecipeLine{}
		for _, p := range parts {
			if id, ok := batchID[p.name]; ok {
				out = append(out, api.RecipeLine{BatchId: &id, Amount: p.amount})
			} else if id, ok := ingID[p.name]; ok {
				out = append(out, api.RecipeLine{IngredientId: &id, Amount: p.amount})
			} else {
				return nil, fmt.Errorf("no ingredient or batch %q", p.name)
			}
		}
		return out, nil
	}

	for _, b := range []struct {
		name     string
		portions float64
		parts    []demoPart
	}{
		{"Chicken curry base", 40, []demoPart{{"Chicken", 8000}, {"Onions", 3000}, {"Tomatoes", 2000},
			{"Cooking oil", 800}, {"Biryani spice mix", 300}, {"Coriander", 4}}},
		{"Mutton curry base", 25, []demoPart{{"Mutton", 5000}, {"Onions", 2000}, {"Tomatoes", 1000},
			{"Cooking oil", 500}, {"Biryani spice mix", 200}}},
		{"Rice pot", 30, []demoPart{{"Basmati rice", 6000}, {"Cooking oil", 300}}},
	} {
		ls, err := lines(b.parts)
		if err != nil {
			return err
		}
		x, err := s.CreateBatch(ctx, orgID, api.BatchInput{Name: b.name, YieldPortions: b.portions, Lines: ls})
		if err != nil {
			return err
		}
		batchID[b.name] = x.Id
	}

	cat, err := s.Catalog(ctx, orgID)
	if err != nil {
		return err
	}
	items := map[string]uuid.UUID{}
	for _, it := range cat.Items {
		items[it.Name] = it.Id
	}
	mods := map[string]uuid.UUID{}
	for _, g := range cat.ModifierGroups {
		for _, m := range g.Modifiers {
			mods[m.Name] = m.Id
		}
	}
	karak := []demoPart{{"Evaporated milk", 30}, {"Tea dust", 3}, {"Sugar", 10}, {"Cardamom", 0.3}, {"Paper cup", 1}}
	for name, parts := range map[string][]demoPart{
		"Karak chai":             karak,
		"Karak, large":           {{"Evaporated milk", 50}, {"Tea dust", 5}, {"Sugar", 15}, {"Cardamom", 0.5}, {"Paper cup", 1}},
		"Sulaimani":              {{"Tea dust", 3}, {"Sugar", 12}, {"Paper cup", 1}},
		"Chicken biryani":        {{"Chicken curry base", 1}, {"Rice pot", 1}, {"Food box", 1}},
		"Mutton biryani":         {{"Mutton curry base", 1}, {"Rice pot", 1}, {"Food box", 1}},
		"Chicken curry and rice": {{"Chicken curry base", 1}, {"Rice pot", 1.2}, {"Food box", 1}},
	} {
		ls, err := lines(parts)
		if err != nil {
			return err
		}
		if _, err := s.SetItemRecipe(ctx, orgID, items[name], ls); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for name, parts := range map[string][]demoPart{
		"Less sugar":  {{"Sugar", -5}},
		"No sugar":    {{"Sugar", -10}},
		"Extra sugar": {{"Sugar", 5}},
		"Extra rice":  {{"Rice pot", 0.5}},
	} {
		ls, err := lines(parts)
		if err != nil {
			return err
		}
		if _, err := s.SetModifierRecipe(ctx, orgID, mods[name], ls); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}
