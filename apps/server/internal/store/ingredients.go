package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ingredientSelect reads ingredients, aliased i, with their price per unit:
// the latest three purchase lines of their products that have a pack size,
// weighted by quantity, so a big buy counts for more than a small one.
const ingredientSelect = `SELECT i.id, i.name, i.unit,
	(SELECT COUNT(*) FROM products p WHERE p.ingredient_id = i.id AND NOT p.archived)::int,
	c.cost, c.last::text
	FROM ingredients i LEFT JOIN LATERAL (
		SELECT (SUM(x.total_fils) / NULLIF(SUM(x.qty_milli::numeric * x.pack_milli / 1000000), 0))::float8 AS cost,
			MAX(x.date) AS last
		FROM (SELECT l.total_fils, l.qty_milli, p.pack_milli, pu.date
			FROM purchase_lines l JOIN products p ON p.id = l.product_id JOIN purchases pu ON pu.id = l.purchase_id
			WHERE p.ingredient_id = i.id AND p.pack_milli IS NOT NULL AND pu.deleted_at IS NULL
			ORDER BY pu.date DESC, pu.created_at DESC, l.position DESC LIMIT 3) x
	) c ON true`

func scanIngredient(r pgx.CollectableRow) (api.Ingredient, error) {
	var x api.Ingredient
	return x, r.Scan(&x.Id, &x.Name, &x.Unit, &x.Products, &x.UnitCostFils, &x.PricedOn)
}

// ListIngredients lists an organization's ingredients, by name.
func (s *Store) ListIngredients(ctx context.Context, orgID uuid.UUID) ([]api.Ingredient, error) {
	rows, err := s.pool.Query(ctx, ingredientSelect+" WHERE i.org_id = $1 AND NOT i.archived ORDER BY lower(i.name)", orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanIngredient)
}

func (s *Store) ingredient(ctx context.Context, orgID, id uuid.UUID) (api.Ingredient, error) {
	rows, err := s.pool.Query(ctx, ingredientSelect+" WHERE i.id = $1 AND i.org_id = $2 AND NOT i.archived", id, orgID)
	if err != nil {
		return api.Ingredient{}, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanIngredient)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// taken turns a unique name's violation into a message.
func taken(err error, what string) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return &Problem{ErrConflict, "Another " + what + " has that name."}
	}
	return err
}

// CreateIngredient adds an ingredient.
func (s *Store) CreateIngredient(ctx context.Context, orgID uuid.UUID, in api.IngredientInput) (api.Ingredient, error) {
	id := newID()
	if _, err := s.pool.Exec(ctx, "INSERT INTO ingredients (id, org_id, name, unit) VALUES ($1, $2, $3, $4)",
		id, orgID, strings.TrimSpace(in.Name), string(in.Unit)); err != nil {
		return api.Ingredient{}, taken(err, "ingredient")
	}
	return s.ingredient(ctx, orgID, id)
}

// UpdateIngredient renames an ingredient, or changes its unit while none of
// its products has a pack size in the old one.
func (s *Store) UpdateIngredient(ctx context.Context, orgID, id uuid.UUID, in api.IngredientInput) (api.Ingredient, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var unit string
		err := tx.QueryRow(ctx, "SELECT unit FROM ingredients WHERE id = $1 AND org_id = $2 AND NOT archived FOR UPDATE",
			id, orgID).Scan(&unit)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if unit != string(in.Unit) {
			var packed int
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM products WHERE ingredient_id = $1 AND NOT archived
				AND pack_milli IS NOT NULL`, id).Scan(&packed); err != nil {
				return err
			}
			if packed > 0 {
				return &Problem{ErrConflict, "Its products' pack sizes are in " + unitWord(unit) +
					". Clear them first, or keep the unit."}
			}
		}
		_, err = tx.Exec(ctx, "UPDATE ingredients SET name = $2, unit = $3, updated_at = now() WHERE id = $1",
			id, strings.TrimSpace(in.Name), string(in.Unit))
		return taken(err, "ingredient")
	})
	if err != nil {
		return api.Ingredient{}, err
	}
	return s.ingredient(ctx, orgID, id)
}

func unitWord(u string) string {
	switch u {
	case "g":
		return "grams"
	case "ml":
		return "millilitres"
	}
	return "pieces"
}

// DeleteIngredient deletes an ingredient; its products stay, belonging to
// none, without pack sizes.
func (s *Store) DeleteIngredient(ctx context.Context, orgID, id uuid.UUID) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		// Recipes would lose it without a word: take it out of them first.
		var used int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM recipe_lines l LEFT JOIN items i ON i.id = l.item_id
			LEFT JOIN modifiers m ON m.id = l.modifier_id LEFT JOIN batches b ON b.id = l.batch_id
			WHERE l.ingredient_id = $1 AND (NOT i.archived OR NOT m.archived OR NOT b.archived)`, id).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return &Problem{ErrConflict, fmt.Sprintf("%d recipes use it. Take it out of them first.", used)}
		}
		tag, err := tx.Exec(ctx, "UPDATE ingredients SET archived = true, updated_at = now() WHERE id = $1 AND org_id = $2 AND NOT archived",
			id, orgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, "UPDATE products SET ingredient_id = NULL, pack_milli = NULL, updated_at = now() WHERE ingredient_id = $1", id); err != nil {
			return err
		}
		return recost(ctx, tx, orgID)
	})
}

// UpdateProduct saves a product's name, area, ingredient and pack size.
func (s *Store) UpdateProduct(ctx context.Context, orgID, id uuid.UUID, in api.ProductInput) (api.Product, error) {
	var pack *int64
	if in.IngredientId != nil && in.PackAmount != nil {
		m := Milli(*in.PackAmount)
		pack = &m
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if in.IngredientId != nil {
			var ok bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM ingredients WHERE id = $1 AND org_id = $2 AND NOT archived)",
				*in.IngredientId, orgID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return &Problem{ErrNotFound, "That ingredient doesn't exist."}
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE products SET name = $3, area = $4, ingredient_id = $5, pack_milli = $6,
			code = $7, updated_at = now() WHERE id = $1 AND org_id = $2 AND NOT archived`,
			id, orgID, strings.TrimSpace(in.Name), string(in.Area), in.IngredientId, pack,
			nilIfEmpty(strings.TrimSpace(deref(in.Code))))
		if err != nil {
			return taken(err, "product")
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// A new pack size or ingredient moves prices, and recipe costs.
		return recost(ctx, tx, orgID)
	})
	if err != nil {
		return api.Product{}, err
	}
	list, err := s.ListProducts(ctx, orgID)
	if err != nil {
		return api.Product{}, err
	}
	for _, p := range list {
		if p.Id == id {
			return p, nil
		}
	}
	return api.Product{}, ErrNotFound
}
