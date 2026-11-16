package store

import (
	"context"
	"errors"
	"fmt"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The menu. Every change takes the organization's catalog version up by
// one, and stamps the rows it changed with the new version, in the same
// transaction: a terminal that has version N needs only rows above N.
// Deleting archives a row, so the change reaches terminals too.

// bump takes the organization's catalog version up, and returns it.
func bump(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int64, error) {
	var v int64
	err := tx.QueryRow(ctx, `UPDATE organizations SET catalog_version = catalog_version + 1
		WHERE id = $1 RETURNING catalog_version`, orgID).Scan(&v)
	return v, err
}

// Catalog returns the organization's menu, without what was deleted.
func (s *Store) Catalog(ctx context.Context, orgID uuid.UUID) (api.Catalog, error) {
	c := api.Catalog{Categories: []api.Category{}, Items: []api.Item{}, ModifierGroups: []api.ModifierGroup{}}
	if err := s.pool.QueryRow(ctx, "SELECT catalog_version FROM organizations WHERE id = $1", orgID).
		Scan(&c.Version); err != nil {
		return c, err
	}

	rows, err := s.pool.Query(ctx, `SELECT id, name, name_ar, color, position, kitchen FROM categories
		WHERE org_id = $1 AND NOT archived ORDER BY position`, orgID)
	if err != nil {
		return c, err
	}
	c.Categories, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Category, error) {
		var x api.Category
		err := r.Scan(&x.Id, &x.Name, &x.NameAr, &x.Color, &x.Position, &x.Kitchen)
		return x, err
	})
	if err != nil {
		return c, err
	}

	groupsOf := map[uuid.UUID][]uuid.UUID{}
	rows, err = s.pool.Query(ctx, `SELECT img.item_id, img.group_id FROM item_modifier_groups img
		JOIN modifier_groups g ON g.id = img.group_id
		WHERE g.org_id = $1 AND NOT g.archived ORDER BY img.item_id, img.position`, orgID)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var item, group uuid.UUID
		if err := rows.Scan(&item, &group); err != nil {
			return c, err
		}
		groupsOf[item] = append(groupsOf[item], group)
	}
	if err := rows.Err(); err != nil {
		return c, err
	}

	rows, err = s.pool.Query(ctx, `SELECT i.id, i.category_id, i.name, i.name_ar, i.price_fils, i.position, i.kitchen
		FROM items i JOIN categories c ON c.id = i.category_id
		WHERE i.org_id = $1 AND NOT i.archived ORDER BY c.position, i.position`, orgID)
	if err != nil {
		return c, err
	}
	c.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Item, error) {
		var x api.Item
		err := r.Scan(&x.Id, &x.CategoryId, &x.Name, &x.NameAr, &x.PriceFils, &x.Position, &x.Kitchen)
		x.ModifierGroupIds = groupsOf[x.Id]
		if x.ModifierGroupIds == nil {
			x.ModifierGroupIds = []uuid.UUID{}
		}
		return x, err
	})
	if err != nil {
		return c, err
	}

	mods := map[uuid.UUID][]api.Modifier{}
	rows, err = s.pool.Query(ctx, `SELECT id, group_id, name, name_ar, price_delta_fils, is_default, position
		FROM modifiers WHERE org_id = $1 AND NOT archived ORDER BY group_id, position`, orgID)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var m api.Modifier
		var group uuid.UUID
		if err := rows.Scan(&m.Id, &group, &m.Name, &m.NameAr, &m.PriceDeltaFils, &m.IsDefault, &m.Position); err != nil {
			return c, err
		}
		mods[group] = append(mods[group], m)
	}
	if err := rows.Err(); err != nil {
		return c, err
	}

	rows, err = s.pool.Query(ctx, `SELECT id, name, name_ar, multiple, position FROM modifier_groups
		WHERE org_id = $1 AND NOT archived ORDER BY position`, orgID)
	if err != nil {
		return c, err
	}
	c.ModifierGroups, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.ModifierGroup, error) {
		var g api.ModifierGroup
		err := r.Scan(&g.Id, &g.Name, &g.NameAr, &g.Multiple, &g.Position)
		g.Modifiers = mods[g.Id]
		if g.Modifiers == nil {
			g.Modifiers = []api.Modifier{}
		}
		return g, err
	})
	return c, err
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Categories.

// CreateCategory adds a category at the end.
func (s *Store) CreateCategory(ctx context.Context, orgID uuid.UUID, in api.CategoryInput) (api.Category, error) {
	c := api.Category{Id: newID(), Name: in.Name, NameAr: str(in.NameAr), Color: in.Color, Kitchen: in.Kitchen}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM categories
			WHERE org_id = $1 AND NOT archived`, orgID).Scan(&c.Position); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO categories (id, org_id, name, name_ar, color, position, kitchen, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, c.Id, orgID, c.Name, c.NameAr, c.Color, c.Position, c.Kitchen, v)
		return err
	})
	return c, err
}

// UpdateCategory saves a category.
func (s *Store) UpdateCategory(ctx context.Context, orgID, id uuid.UUID, in api.CategoryInput) (api.Category, error) {
	c := api.Category{Id: id, Name: in.Name, NameAr: str(in.NameAr), Color: in.Color, Kitchen: in.Kitchen}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `UPDATE categories SET name = $3, name_ar = $4, color = $5, kitchen = $6,
			version = $7, updated_at = now() WHERE id = $1 AND org_id = $2 AND NOT archived RETURNING position`,
			id, orgID, c.Name, c.NameAr, c.Color, c.Kitchen, v).Scan(&c.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return c, err
}

// DeleteCategory deletes a category that has no items.
func (s *Store) DeleteCategory(ctx context.Context, orgID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var items int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE category_id = $1 AND org_id = $2 AND NOT archived",
			id, orgID).Scan(&items); err != nil {
			return err
		}
		if items > 0 {
			return &Problem{ErrConflict, fmt.Sprintf("This category still has %d items. Move or delete them first.", items)}
		}
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE categories SET archived = true, version = $3, updated_at = now()
			WHERE id = $1 AND org_id = $2 AND NOT archived`, id, orgID, v)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return renumber(ctx, tx, "categories", "org_id = $1 AND NOT archived", []any{orgID}, uuid.Nil, 0, v)
	})
}

// MoveCategory moves a category to a position.
func (s *Store) MoveCategory(ctx context.Context, orgID, id uuid.UUID, position int) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		return renumber(ctx, tx, "categories", "org_id = $1 AND NOT archived", []any{orgID}, id, position, v)
	})
}

// renumber gives the rows of table matching where the positions 0, 1, 2…
// in their current order, with id moved to position (unless id is Nil). The
// rows whose position changes get version v.
func renumber(ctx context.Context, tx pgx.Tx, table, where string, args []any, id uuid.UUID, position int, v int64) error {
	rows, err := tx.Query(ctx, "SELECT id, position FROM "+table+" WHERE "+where+" ORDER BY position, id", args...)
	if err != nil {
		return err
	}
	type row struct {
		id  uuid.UUID
		pos int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.id, &x.pos)
		return x, err
	})
	if err != nil {
		return err
	}
	if id != uuid.Nil {
		from := -1
		for i, x := range list {
			if x.id == id {
				from = i
			}
		}
		if from < 0 {
			return ErrNotFound
		}
		moved := list[from]
		list = append(list[:from], list[from+1:]...)
		position = min(max(position, 0), len(list))
		list = append(list[:position], append([]row{moved}, list[position:]...)...)
	}
	for i, x := range list {
		if x.pos == i {
			continue
		}
		if _, err := tx.Exec(ctx, "UPDATE "+table+" SET position = $2, version = $3, updated_at = now() WHERE id = $1",
			x.id, i, v); err != nil {
			return err
		}
	}
	return nil
}

// Items.

// checkGroups makes sure the groups belong to the organization and aren't
// deleted.
func checkGroups(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(DISTINCT id) FROM modifier_groups
		WHERE org_id = $1 AND NOT archived AND id = ANY($2)`, orgID, ids).Scan(&n); err != nil {
		return err
	}
	if n != len(dedupe(ids)) {
		return &Problem{ErrNotFound, "One of the option groups doesn't exist any more."}
	}
	return nil
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func checkCategory(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) error {
	var ok bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1 AND org_id = $2 AND NOT archived)",
		id, orgID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return &Problem{ErrNotFound, "That category doesn't exist any more."}
	}
	return nil
}

func setItemGroups(ctx context.Context, tx pgx.Tx, itemID uuid.UUID, groups []uuid.UUID) error {
	if _, err := tx.Exec(ctx, "DELETE FROM item_modifier_groups WHERE item_id = $1", itemID); err != nil {
		return err
	}
	for i, g := range dedupe(groups) {
		if _, err := tx.Exec(ctx, "INSERT INTO item_modifier_groups (item_id, group_id, position) VALUES ($1, $2, $3)",
			itemID, g, i); err != nil {
			return err
		}
	}
	return nil
}

// CreateItem adds an item at the end of its category.
func (s *Store) CreateItem(ctx context.Context, orgID uuid.UUID, in api.ItemInput) (api.Item, error) {
	it := api.Item{Id: newID(), CategoryId: in.CategoryId, Name: in.Name, NameAr: str(in.NameAr),
		PriceFils: in.PriceFils, Kitchen: in.Kitchen, ModifierGroupIds: dedupe(in.ModifierGroupIds)}
	if it.ModifierGroupIds == nil {
		it.ModifierGroupIds = []uuid.UUID{}
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := checkCategory(ctx, tx, orgID, in.CategoryId); err != nil {
			return err
		}
		if err := checkGroups(ctx, tx, orgID, in.ModifierGroupIds); err != nil {
			return err
		}
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM items
			WHERE category_id = $1 AND NOT archived`, in.CategoryId).Scan(&it.Position); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO items (id, org_id, category_id, name, name_ar, price_fils, position,
			kitchen, version) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			it.Id, orgID, it.CategoryId, it.Name, it.NameAr, it.PriceFils, it.Position, it.Kitchen, v); err != nil {
			return err
		}
		return setItemGroups(ctx, tx, it.Id, it.ModifierGroupIds)
	})
	return it, err
}

// UpdateItem saves an item. Moving it to another category puts it at the
// end there.
func (s *Store) UpdateItem(ctx context.Context, orgID, id uuid.UUID, in api.ItemInput) (api.Item, error) {
	it := api.Item{Id: id, CategoryId: in.CategoryId, Name: in.Name, NameAr: str(in.NameAr),
		PriceFils: in.PriceFils, Kitchen: in.Kitchen, ModifierGroupIds: dedupe(in.ModifierGroupIds)}
	if it.ModifierGroupIds == nil {
		it.ModifierGroupIds = []uuid.UUID{}
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var oldCategory uuid.UUID
		err := tx.QueryRow(ctx, "SELECT category_id, position FROM items WHERE id = $1 AND org_id = $2 AND NOT archived FOR UPDATE",
			id, orgID).Scan(&oldCategory, &it.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := checkCategory(ctx, tx, orgID, in.CategoryId); err != nil {
			return err
		}
		if err := checkGroups(ctx, tx, orgID, in.ModifierGroupIds); err != nil {
			return err
		}
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if oldCategory != in.CategoryId {
			if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM items
				WHERE category_id = $1 AND NOT archived`, in.CategoryId).Scan(&it.Position); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE items SET category_id = $3, name = $4, name_ar = $5, price_fils = $6,
			position = $7, kitchen = $8, version = $9, updated_at = now() WHERE id = $1 AND org_id = $2`,
			id, orgID, it.CategoryId, it.Name, it.NameAr, it.PriceFils, it.Position, it.Kitchen, v); err != nil {
			return err
		}
		if err := setItemGroups(ctx, tx, id, it.ModifierGroupIds); err != nil {
			return err
		}
		if oldCategory != in.CategoryId {
			return renumber(ctx, tx, "items", "category_id = $1 AND NOT archived", []any{oldCategory}, uuid.Nil, 0, v)
		}
		return nil
	})
	return it, err
}

// DeleteItem deletes an item.
func (s *Store) DeleteItem(ctx context.Context, orgID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		var category uuid.UUID
		err = tx.QueryRow(ctx, `UPDATE items SET archived = true, version = $3, updated_at = now()
			WHERE id = $1 AND org_id = $2 AND NOT archived RETURNING category_id`, id, orgID, v).Scan(&category)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return renumber(ctx, tx, "items", "category_id = $1 AND NOT archived", []any{category}, uuid.Nil, 0, v)
	})
}

// MoveItem moves an item to a position within its category.
func (s *Store) MoveItem(ctx context.Context, orgID, id uuid.UUID, position int) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var category uuid.UUID
		err := tx.QueryRow(ctx, "SELECT category_id FROM items WHERE id = $1 AND org_id = $2 AND NOT archived",
			id, orgID).Scan(&category)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		return renumber(ctx, tx, "items", "category_id = $1 AND NOT archived", []any{category}, id, position, v)
	})
}

// Option groups.

// CreateModifierGroup adds an option group at the end.
func (s *Store) CreateModifierGroup(ctx context.Context, orgID uuid.UUID, in api.ModifierGroupInput) (api.ModifierGroup, error) {
	g := api.ModifierGroup{Id: newID(), Name: in.Name, NameAr: str(in.NameAr), Multiple: in.Multiple}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM modifier_groups
			WHERE org_id = $1 AND NOT archived`, orgID).Scan(&g.Position); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO modifier_groups (id, org_id, name, name_ar, multiple, position, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, g.Id, orgID, g.Name, g.NameAr, g.Multiple, g.Position, v); err != nil {
			return err
		}
		g.Modifiers, err = saveModifiers(ctx, tx, orgID, g.Id, in.Modifiers, v)
		return err
	})
	return g, err
}

// UpdateModifierGroup saves an option group and its modifiers; modifiers
// left out are deleted.
func (s *Store) UpdateModifierGroup(ctx context.Context, orgID, id uuid.UUID, in api.ModifierGroupInput) (api.ModifierGroup, error) {
	g := api.ModifierGroup{Id: id, Name: in.Name, NameAr: str(in.NameAr), Multiple: in.Multiple}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `UPDATE modifier_groups SET name = $3, name_ar = $4, multiple = $5, version = $6,
			updated_at = now() WHERE id = $1 AND org_id = $2 AND NOT archived RETURNING position`,
			id, orgID, g.Name, g.NameAr, g.Multiple, v).Scan(&g.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		g.Modifiers, err = saveModifiers(ctx, tx, orgID, id, in.Modifiers, v)
		return err
	})
	return g, err
}

func saveModifiers(ctx context.Context, tx pgx.Tx, orgID, groupID uuid.UUID, in []api.ModifierInput, v int64) ([]api.Modifier, error) {
	existing := map[uuid.UUID]bool{}
	rows, err := tx.Query(ctx, "SELECT id FROM modifiers WHERE group_id = $1 AND NOT archived", groupID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		existing[id] = true
	}
	out := make([]api.Modifier, 0, len(in))
	kept := map[uuid.UUID]bool{}
	for i, m := range in {
		mod := api.Modifier{Name: m.Name, NameAr: str(m.NameAr), PriceDeltaFils: m.PriceDeltaFils,
			IsDefault: m.IsDefault, Position: i}
		if m.Id != nil && existing[*m.Id] && !kept[*m.Id] {
			mod.Id = *m.Id
			kept[mod.Id] = true
			if _, err := tx.Exec(ctx, `UPDATE modifiers SET name = $2, name_ar = $3, price_delta_fils = $4,
				is_default = $5, position = $6, version = $7, updated_at = now() WHERE id = $1`,
				mod.Id, mod.Name, mod.NameAr, mod.PriceDeltaFils, mod.IsDefault, mod.Position, v); err != nil {
				return nil, err
			}
		} else {
			mod.Id = newID()
			if _, err := tx.Exec(ctx, `INSERT INTO modifiers (id, org_id, group_id, name, name_ar, price_delta_fils,
				is_default, position, version) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				mod.Id, orgID, groupID, mod.Name, mod.NameAr, mod.PriceDeltaFils, mod.IsDefault, mod.Position, v); err != nil {
				return nil, err
			}
		}
		out = append(out, mod)
	}
	for id := range existing {
		if !kept[id] {
			if _, err := tx.Exec(ctx, "UPDATE modifiers SET archived = true, version = $2, updated_at = now() WHERE id = $1",
				id, v); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// DeleteModifierGroup deletes an option group, and takes it off the items
// that had it.
func (s *Store) DeleteModifierGroup(ctx context.Context, orgID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE modifier_groups SET archived = true, version = $3, updated_at = now()
			WHERE id = $1 AND org_id = $2 AND NOT archived`, id, orgID, v)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, "UPDATE modifiers SET archived = true, version = $2, updated_at = now() WHERE group_id = $1 AND NOT archived",
			id, v); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE items SET version = $2, updated_at = now()
			WHERE id IN (SELECT item_id FROM item_modifier_groups WHERE group_id = $1)`, id, v); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM item_modifier_groups WHERE group_id = $1", id); err != nil {
			return err
		}
		return renumber(ctx, tx, "modifier_groups", "org_id = $1 AND NOT archived", []any{orgID}, uuid.Nil, 0, v)
	})
}

// MoveModifierGroup moves an option group to a position.
func (s *Store) MoveModifierGroup(ctx context.Context, orgID, id uuid.UUID, position int) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		return renumber(ctx, tx, "modifier_groups", "org_id = $1 AND NOT archived", []any{orgID}, id, position, v)
	})
}
