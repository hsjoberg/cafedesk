package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Recipes: what goes into items, options and batches, and what that costs
// at today's ingredient prices. An item or option with a recipe gets its
// cost from it: recost writes it into the cost history whenever a recipe,
// a batch or a price changes, so sales keep the cost they had when made.

type recipeLine struct {
	ingredient, batch *uuid.UUID
	amount            int64 // thousandths
}

type batchInfo struct {
	id    uuid.UUID
	name  string
	yield int64 // thousandths of a portion
	lines []recipeLine
}

// costBook is what costing needs: each ingredient's price per unit, the
// batches, and every recipe's lines.
type costBook struct {
	price     map[uuid.UUID]*float64 // fils per unit; nil while it has none
	name      map[uuid.UUID]string
	batches   map[uuid.UUID]*batchInfo
	order     []uuid.UUID // batches by name
	items     map[uuid.UUID][]recipeLine
	modifiers map[uuid.UUID][]recipeLine
}

func loadBook(ctx context.Context, q pgx.Tx, orgID uuid.UUID) (*costBook, error) {
	b := &costBook{price: map[uuid.UUID]*float64{}, name: map[uuid.UUID]string{}, batches: map[uuid.UUID]*batchInfo{},
		items: map[uuid.UUID][]recipeLine{}, modifiers: map[uuid.UUID][]recipeLine{}}
	rows, err := q.Query(ctx, ingredientSelect+" WHERE i.org_id = $1 AND NOT i.archived", orgID)
	if err != nil {
		return nil, err
	}
	ings, err := pgx.CollectRows(rows, scanIngredient)
	if err != nil {
		return nil, err
	}
	for _, i := range ings {
		b.price[i.Id], b.name[i.Id] = i.UnitCostFils, i.Name
	}
	rows, err = q.Query(ctx, "SELECT id, name, yield_milli FROM batches WHERE org_id = $1 AND NOT archived ORDER BY lower(name)", orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		x := &batchInfo{}
		if err := rows.Scan(&x.id, &x.name, &x.yield); err != nil {
			return nil, err
		}
		b.batches[x.id] = x
		b.order = append(b.order, x.id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Lines of items and options still on the menu, and of batches still kept.
	rows, err = q.Query(ctx, `SELECT l.item_id, l.modifier_id, l.batch_id, l.ingredient_id, l.part_batch_id, l.amount_milli
		FROM recipe_lines l
		LEFT JOIN items i ON i.id = l.item_id
		LEFT JOIN modifiers m ON m.id = l.modifier_id
		LEFT JOIN batches b ON b.id = l.batch_id
		WHERE l.org_id = $1 AND (NOT i.archived OR NOT m.archived OR NOT b.archived)
		ORDER BY l.position`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item, mod, batch *uuid.UUID
		var l recipeLine
		if err := rows.Scan(&item, &mod, &batch, &l.ingredient, &l.batch, &l.amount); err != nil {
			return nil, err
		}
		switch {
		case item != nil:
			b.items[*item] = append(b.items[*item], l)
		case mod != nil:
			b.modifiers[*mod] = append(b.modifiers[*mod], l)
		case batch != nil:
			if x := b.batches[*batch]; x != nil {
				x.lines = append(x.lines, l)
			}
		}
	}
	return b, rows.Err()
}

// cost is what lines cost in fils, and the ingredients without a price.
func (b *costBook) cost(lines []recipeLine) (float64, []string) {
	var total float64
	missing := map[string]bool{}
	for _, l := range lines {
		amount := float64(l.amount) / 1000
		switch {
		case l.ingredient != nil:
			if p := b.price[*l.ingredient]; p != nil {
				total += amount * *p
			} else {
				missing[b.name[*l.ingredient]] = true
			}
		case l.batch != nil:
			portion, m := b.portion(*l.batch)
			total += amount * portion
			for _, n := range m {
				missing[n] = true
			}
		}
	}
	out := make([]string, 0, len(missing))
	for n := range missing {
		out = append(out, n)
	}
	sort.Strings(out)
	return total, out
}

// portion is what one portion of a batch costs.
func (b *costBook) portion(id uuid.UUID) (float64, []string) {
	x := b.batches[id]
	if x == nil || x.yield <= 0 {
		return 0, nil
	}
	total, missing := b.cost(x.lines)
	return total / (float64(x.yield) / 1000), missing
}

// costOf is a recipe's cost in whole fils, nil while a price is missing.
func (b *costBook) costOf(lines []recipeLine) (*int64, []string) {
	c, missing := b.cost(lines)
	if len(missing) > 0 {
		return nil, missing
	}
	f := int64(math.Round(c))
	return &f, missing
}

// recost writes each recipe's cost now into the cost history of its item or
// option, where it changed.
func recost(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	b, err := loadBook(ctx, tx, orgID)
	if err != nil {
		return err
	}
	for id, lines := range b.items {
		c, _ := b.costOf(lines)
		if err := setCost(ctx, tx, itemCosts, orgID, id, c); err != nil {
			return err
		}
	}
	for id, lines := range b.modifiers {
		c, _ := b.costOf(lines)
		if err := setCost(ctx, tx, modifierCosts, orgID, id, c); err != nil {
			return err
		}
	}
	return nil
}

func apiLines(lines []recipeLine) []api.RecipeLine {
	out := make([]api.RecipeLine, len(lines))
	for i, l := range lines {
		out[i] = api.RecipeLine{IngredientId: l.ingredient, BatchId: l.batch, Amount: Unmilli(l.amount)}
	}
	return out
}

// Recipes reads every recipe, with what each costs now.
func (s *Store) Recipes(ctx context.Context, orgID uuid.UUID) (api.Recipes, error) {
	out := api.Recipes{Items: []api.ItemRecipe{}, Modifiers: []api.ModifierRecipe{}, Batches: []api.Batch{}}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		b, err := loadBook(ctx, tx, orgID)
		if err != nil {
			return err
		}
		for id, lines := range b.items {
			c, missing := b.costOf(lines)
			out.Items = append(out.Items, api.ItemRecipe{ItemId: id, Lines: apiLines(lines), CostFils: c, Missing: missing})
		}
		for id, lines := range b.modifiers {
			c, missing := b.costOf(lines)
			out.Modifiers = append(out.Modifiers, api.ModifierRecipe{ModifierId: id, Lines: apiLines(lines), CostFils: c, Missing: missing})
		}
		used, err := batchUse(ctx, tx, orgID)
		if err != nil {
			return err
		}
		for _, id := range b.order {
			out.Batches = append(out.Batches, b.apiBatch(id, used[id]))
		}
		return nil
	})
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].ItemId.String() < out.Items[j].ItemId.String() })
	sort.Slice(out.Modifiers, func(i, j int) bool {
		return out.Modifiers[i].ModifierId.String() < out.Modifiers[j].ModifierId.String()
	})
	return out, err
}

func (b *costBook) apiBatch(id uuid.UUID, used int) api.Batch {
	x := b.batches[id]
	c, missing := b.costOf(x.lines)
	out := api.Batch{Id: x.id, Name: x.name, YieldPortions: Unmilli(x.yield), Lines: apiLines(x.lines), CostFils: c,
		Missing: missing, UsedBy: used}
	if out.Lines == nil {
		out.Lines = []api.RecipeLine{}
	}
	if c != nil {
		p, _ := b.portion(id)
		out.PortionCostFils = &p
	}
	return out
}

// batchUse counts the items and options on the menu that use each batch.
func batchUse(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := tx.Query(ctx, `SELECT l.part_batch_id, COUNT(DISTINCT COALESCE(l.item_id, l.modifier_id))::int
		FROM recipe_lines l LEFT JOIN items i ON i.id = l.item_id LEFT JOIN modifiers m ON m.id = l.modifier_id
		WHERE l.org_id = $1 AND l.part_batch_id IS NOT NULL AND (NOT i.archived OR NOT m.archived)
		GROUP BY l.part_batch_id`, orgID)
	if err != nil {
		return nil, err
	}
	used := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		used[id] = n
	}
	return used, rows.Err()
}

// owner is what a recipe belongs to: one of an item, an option or a batch.
type owner struct {
	column string // item_id, modifier_id or batch_id
	id     uuid.UUID
}

// writeLines replaces a recipe's lines, checking each names an ingredient or
// batch of the organization.
func writeLines(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, o owner, lines []api.RecipeLine) error {
	if _, err := tx.Exec(ctx, "DELETE FROM recipe_lines WHERE "+o.column+" = $1", o.id); err != nil {
		return err
	}
	for i, l := range lines {
		var ok bool
		var err error
		if l.IngredientId != nil {
			err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM ingredients WHERE id = $1 AND org_id = $2 AND NOT archived)",
				*l.IngredientId, orgID).Scan(&ok)
		} else {
			err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM batches WHERE id = $1 AND org_id = $2 AND NOT archived)",
				*l.BatchId, orgID).Scan(&ok)
		}
		if err != nil {
			return err
		}
		if !ok {
			return &Problem{ErrNotFound, "One of the ingredients or batches doesn't exist."}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO recipe_lines (id, org_id, `+o.column+`, position, ingredient_id, part_batch_id,
			amount_milli) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			newID(), orgID, o.id, i, l.IngredientId, l.BatchId, Milli(l.Amount)); err != nil {
			return err
		}
	}
	return nil
}

// SetItemRecipe sets what goes into an item, and its cost from it.
func (s *Store) SetItemRecipe(ctx context.Context, orgID, itemID uuid.UUID, lines []api.RecipeLine) (api.ItemRecipe, error) {
	out := api.ItemRecipe{ItemId: itemID, Lines: []api.RecipeLine{}, Missing: []string{}}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM items WHERE id = $1 AND org_id = $2 AND NOT archived)",
			itemID, orgID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if err := writeLines(ctx, tx, orgID, owner{"item_id", itemID}, lines); err != nil {
			return err
		}
		if err := recost(ctx, tx, orgID); err != nil {
			return err
		}
		b, err := loadBook(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if l := b.items[itemID]; l != nil {
			out.Lines = apiLines(l)
			out.CostFils, out.Missing = b.costOf(l)
		}
		return nil
	})
	return out, err
}

// SetModifierRecipe sets how an option changes its item's recipe.
func (s *Store) SetModifierRecipe(ctx context.Context, orgID, modID uuid.UUID, lines []api.RecipeLine) (api.ModifierRecipe, error) {
	out := api.ModifierRecipe{ModifierId: modID, Lines: []api.RecipeLine{}, Missing: []string{}}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM modifiers WHERE id = $1 AND org_id = $2 AND NOT archived)",
			modID, orgID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if err := writeLines(ctx, tx, orgID, owner{"modifier_id", modID}, lines); err != nil {
			return err
		}
		if err := recost(ctx, tx, orgID); err != nil {
			return err
		}
		b, err := loadBook(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if l := b.modifiers[modID]; l != nil {
			out.Lines = apiLines(l)
			out.CostFils, out.Missing = b.costOf(l)
		}
		return nil
	})
	return out, err
}

// CreateBatch adds a batch; UpdateBatch saves one.
func (s *Store) CreateBatch(ctx context.Context, orgID uuid.UUID, in api.BatchInput) (api.Batch, error) {
	return s.saveBatch(ctx, orgID, nil, in)
}

func (s *Store) UpdateBatch(ctx context.Context, orgID, id uuid.UUID, in api.BatchInput) (api.Batch, error) {
	return s.saveBatch(ctx, orgID, &id, in)
}

func (s *Store) saveBatch(ctx context.Context, orgID uuid.UUID, id *uuid.UUID, in api.BatchInput) (api.Batch, error) {
	var out api.Batch
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		name := strings.TrimSpace(in.Name)
		var bid uuid.UUID
		if id == nil {
			bid = newID()
			if _, err := tx.Exec(ctx, "INSERT INTO batches (id, org_id, name, yield_milli) VALUES ($1, $2, $3, $4)",
				bid, orgID, name, Milli(in.YieldPortions)); err != nil {
				return taken(err, "batch")
			}
		} else {
			bid = *id
			tag, err := tx.Exec(ctx, `UPDATE batches SET name = $3, yield_milli = $4, updated_at = now()
				WHERE id = $1 AND org_id = $2 AND NOT archived`, bid, orgID, name, Milli(in.YieldPortions))
			if err != nil {
				return taken(err, "batch")
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		if err := writeLines(ctx, tx, orgID, owner{"batch_id", bid}, in.Lines); err != nil {
			return err
		}
		if err := recost(ctx, tx, orgID); err != nil {
			return err
		}
		b, err := loadBook(ctx, tx, orgID)
		if err != nil {
			return err
		}
		used, err := batchUse(ctx, tx, orgID)
		if err != nil {
			return err
		}
		out = b.apiBatch(bid, used[bid])
		return nil
	})
	return out, err
}

// DeleteBatch deletes a batch no recipe on the menu uses.
func (s *Store) DeleteBatch(ctx context.Context, orgID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		used, err := batchUse(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if n := used[id]; n > 0 {
			return &Problem{ErrConflict, fmt.Sprintf("%d recipes use it. Take it out of them first.", n)}
		}
		tag, err := tx.Exec(ctx, "UPDATE batches SET archived = true, updated_at = now() WHERE id = $1 AND org_id = $2 AND NOT archived",
			id, orgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// hasRecipe reports whether an item's or option's cost comes from a recipe.
func hasRecipe(ctx context.Context, tx pgx.Tx, column string, id uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM recipe_lines WHERE "+column+" = $1)", id).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}
