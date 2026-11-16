package store

import (
	"context"
	"errors"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Till staff. Changing someone takes the catalog version up, as a menu
// change does, so tills pull it with the menu.

// ListStaff lists the organization's till staff, active first.
func (s *Store) ListStaff(ctx context.Context, orgID uuid.UUID) ([]api.Staff, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id, s.name, s.role, s.active, s.must_change_pin,
		COALESCE(array_agg(ss.shop_id) FILTER (WHERE ss.shop_id IS NOT NULL), '{}')
		FROM staff s LEFT JOIN staff_shops ss ON ss.staff_id = s.id
		WHERE s.org_id = $1 GROUP BY s.id ORDER BY s.active DESC, lower(s.name)`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Staff, error) {
		var st api.Staff
		var role string
		err := r.Scan(&st.Id, &st.Name, &role, &st.Active, &st.MustChangePin, &st.ShopIds)
		st.Role = api.StaffRole(role)
		return st, err
	})
}

// PINHashes returns the PIN hashes of the organization's active staff, but
// one, to keep PINs apart.
func (s *Store) PINHashes(ctx context.Context, orgID, except uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT pin_hash FROM staff WHERE org_id = $1 AND active AND id != $2", orgID, except)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func checkShops(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, shops []uuid.UUID) error {
	var n int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM shops WHERE org_id = $1 AND id = ANY($2)", orgID, dedupe(shops)).
		Scan(&n); err != nil {
		return err
	}
	if n != len(dedupe(shops)) {
		return &Problem{ErrNotFound, "One of the shops doesn't exist."}
	}
	return nil
}

func setStaffShops(ctx context.Context, tx pgx.Tx, staffID uuid.UUID, shops []uuid.UUID) error {
	if _, err := tx.Exec(ctx, "DELETE FROM staff_shops WHERE staff_id = $1", staffID); err != nil {
		return err
	}
	for _, sh := range dedupe(shops) {
		if _, err := tx.Exec(ctx, "INSERT INTO staff_shops (staff_id, shop_id) VALUES ($1, $2)", staffID, sh); err != nil {
			return err
		}
	}
	return nil
}

// CreateStaff adds a person with a starting PIN, which they replace at
// their first sign-in.
func (s *Store) CreateStaff(ctx context.Context, orgID uuid.UUID, in api.StaffInput, pinHash string) (api.Staff, error) {
	st := api.Staff{Id: newID(), Name: in.Name, Role: in.Role, ShopIds: dedupe(in.ShopIds), Active: in.Active,
		MustChangePin: true}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := checkShops(ctx, tx, orgID, in.ShopIds); err != nil {
			return err
		}
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO staff (id, org_id, name, role, pin_hash, must_change_pin, active, version)
			VALUES ($1, $2, $3, $4, $5, true, $6, $7)`, st.Id, orgID, st.Name, string(st.Role), pinHash, st.Active, v); err != nil {
			return err
		}
		return setStaffShops(ctx, tx, st.Id, st.ShopIds)
	})
	return st, err
}

// UpdateStaff saves a person. A PIN hash, if given, becomes their new
// starting PIN.
func (s *Store) UpdateStaff(ctx context.Context, orgID, id uuid.UUID, in api.StaffInput, pinHash *string) (api.Staff, error) {
	st := api.Staff{Id: id, Name: in.Name, Role: in.Role, ShopIds: dedupe(in.ShopIds), Active: in.Active}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if err := checkShops(ctx, tx, orgID, in.ShopIds); err != nil {
			return err
		}
		v, err := bump(ctx, tx, orgID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `UPDATE staff SET name = $3, role = $4, active = $5,
			pin_hash = COALESCE($6::text, pin_hash), must_change_pin = (must_change_pin OR $6::text IS NOT NULL),
			version = $7, updated_at = now() WHERE id = $1 AND org_id = $2 RETURNING must_change_pin`,
			id, orgID, st.Name, string(st.Role), st.Active, pinHash, v).Scan(&st.MustChangePin)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return setStaffShops(ctx, tx, id, st.ShopIds)
	})
	return st, err
}
