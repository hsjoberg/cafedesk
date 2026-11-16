package store

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Areas lists where purchases go, in the order the admin shows them.
var Areas = []api.Area{api.Counter, api.Kitchen, api.Packaging, api.Other}

// Milli turns a quantity into thousandths, as kept; Unmilli turns it back.
func Milli(q float64) int64   { return int64(math.Round(q * 1000)) }
func Unmilli(m int64) float64 { return float64(m) / 1000 }
func nameKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ListSuppliers lists an organization's suppliers, by name.
func (s *Store) ListSuppliers(ctx context.Context, orgID uuid.UUID) ([]api.Supplier, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, name FROM suppliers WHERE org_id = $1 AND NOT archived ORDER BY lower(name)", orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Supplier, error) {
		var x api.Supplier
		return x, r.Scan(&x.Id, &x.Name)
	})
}

// ListProducts lists what an organization buys, by name, each with its
// latest purchase.
func (s *Store) ListProducts(ctx context.Context, orgID uuid.UUID) ([]api.Product, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.name, p.area, last.unit_price_fils, last.qty_milli, last.date::text
		FROM products p LEFT JOIN LATERAL (
			SELECT l.unit_price_fils, l.qty_milli, pu.date FROM purchase_lines l JOIN purchases pu ON pu.id = l.purchase_id
			WHERE l.product_id = p.id AND pu.deleted_at IS NULL
			ORDER BY pu.date DESC, pu.created_at DESC, l.position DESC LIMIT 1) last ON true
		WHERE p.org_id = $1 AND NOT p.archived ORDER BY lower(p.name)`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Product, error) {
		var x api.Product
		var qty *int64
		err := r.Scan(&x.Id, &x.Name, &x.Area, &x.LastUnitPriceFils, &qty, &x.LastPurchasedOn)
		if qty != nil {
			q := Unmilli(*qty)
			x.LastQuantity = &q
		}
		return x, err
	})
}

// supplierFor finds or adds the supplier a purchase names, or returns nil
// for none.
func supplierFor(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, in api.PurchaseInput) (*uuid.UUID, error) {
	if in.SupplierId != nil {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1 AND org_id = $2 AND NOT archived)",
			*in.SupplierId, orgID).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, &Problem{ErrNotFound, "That supplier doesn't exist."}
		}
		return in.SupplierId, nil
	}
	name := strings.TrimSpace(deref(in.SupplierName))
	if name == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT id FROM suppliers WHERE org_id = $1 AND lower(name) = $2 AND NOT archived",
		orgID, nameKey(name)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id = newID()
		_, err = tx.Exec(ctx, "INSERT INTO suppliers (id, org_id, name) VALUES ($1, $2, $3)", id, orgID, name)
	}
	return &id, err
}

// productFor finds or adds a line's product, and makes the line's area the
// product's, so the next purchase of it starts there.
func productFor(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, l api.PurchaseLineInput) (uuid.UUID, string, error) {
	var id uuid.UUID
	var name string
	var err error
	if l.ProductId != nil {
		err = tx.QueryRow(ctx, "SELECT id, name FROM products WHERE id = $1 AND org_id = $2 AND NOT archived",
			*l.ProductId, orgID).Scan(&id, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return id, "", &Problem{ErrNotFound, "One of the products doesn't exist."}
		}
	} else {
		name = strings.TrimSpace(deref(l.ProductName))
		err = tx.QueryRow(ctx, "SELECT id, name FROM products WHERE org_id = $1 AND lower(name) = $2 AND NOT archived",
			orgID, nameKey(name)).Scan(&id, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			id = newID()
			_, err = tx.Exec(ctx, "INSERT INTO products (id, org_id, name, area) VALUES ($1, $2, $3, $4)", id, orgID, name, string(l.Area))
			return id, name, err
		}
	}
	if err != nil {
		return id, name, err
	}
	_, err = tx.Exec(ctx, "UPDATE products SET area = $2, updated_at = now() WHERE id = $1 AND area <> $2", id, string(l.Area))
	return id, name, err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// savePurchase writes a purchase's supplier, total and lines. The purchase
// row must exist.
func savePurchase(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, in api.PurchaseInput) error {
	var shopOK bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM shops WHERE id = $1 AND org_id = $2)", in.ShopId, orgID).
		Scan(&shopOK); err != nil {
		return err
	}
	if !shopOK {
		return &Problem{ErrNotFound, "There's no such shop."}
	}
	supplier, err := supplierFor(ctx, tx, orgID, in)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM purchase_lines WHERE purchase_id = $1", id); err != nil {
		return err
	}
	var total int64
	for i, l := range in.Lines {
		product, _, err := productFor(ctx, tx, orgID, l)
		if err != nil {
			return err
		}
		total += l.TotalFils
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_lines (purchase_id, position, product_id, area, qty_milli,
			unit_price_fils, total_fils) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, i, product, string(l.Area), Milli(l.Quantity), l.UnitPriceFils, l.TotalFils); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE purchases SET shop_id = $2, date = $3::date, supplier_id = $4, reference = $5, total_fils = $6
		WHERE id = $1`, id, in.ShopId, in.Date, supplier, strings.TrimSpace(deref(in.Reference)), total)
	return err
}

// CreatePurchase records a purchase, by a user of the organization.
func (s *Store) CreatePurchase(ctx context.Context, orgID, userID uuid.UUID, in api.PurchaseInput, now time.Time) (api.Purchase, error) {
	id := newID()
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO purchases (id, org_id, shop_id, date, total_fils, created_at, created_by)
			SELECT $1, $2, sh.id, $4::date, 0, $5, $6 FROM shops sh WHERE sh.id = $3 AND sh.org_id = $2`,
			id, orgID, in.ShopId, in.Date, now, userID); err != nil {
			return err
		}
		return savePurchase(ctx, tx, orgID, id, in)
	})
	if err != nil {
		return api.Purchase{}, err
	}
	return s.purchase(ctx, orgID, id)
}

// UpdatePurchase saves a purchase; its lines are replaced.
func (s *Store) UpdatePurchase(ctx context.Context, orgID, userID, id uuid.UUID, in api.PurchaseInput, now time.Time) (api.Purchase, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE purchases SET updated_at = $3, updated_by = $4
			WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`, id, orgID, now, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return savePurchase(ctx, tx, orgID, id, in)
	})
	if err != nil {
		return api.Purchase{}, err
	}
	return s.purchase(ctx, orgID, id)
}

// DeletePurchase marks a purchase deleted.
func (s *Store) DeletePurchase(ctx context.Context, orgID, userID, id uuid.UUID, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE purchases SET deleted_at = $3, deleted_by = $4
		WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`, id, orgID, now, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

const purchaseSelect = `SELECT p.id, p.shop_id, p.date::text, p.supplier_id, s.name, p.reference, p.total_fils,
	p.created_at, cu.name, p.updated_at, uu.name
	FROM purchases p JOIN users cu ON cu.id = p.created_by LEFT JOIN users uu ON uu.id = p.updated_by
	LEFT JOIN suppliers s ON s.id = p.supplier_id`

// purchases reads purchases and their lines.
func (s *Store) purchases(ctx context.Context, q pgx.Tx, where string, args ...any) ([]api.Purchase, error) {
	rows, err := q.Query(ctx, purchaseSelect+" WHERE "+where+" ORDER BY p.date DESC, p.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Purchase, error) {
		var x api.Purchase
		var supplierID *uuid.UUID
		var supplierName *string
		err := r.Scan(&x.Id, &x.ShopId, &x.Date, &supplierID, &supplierName, &x.Reference, &x.TotalFils,
			&x.CreatedAt, &x.CreatedByName, &x.UpdatedAt, &x.UpdatedByName)
		if supplierID != nil {
			x.Supplier = &api.Supplier{Id: *supplierID, Name: deref(supplierName)}
		}
		x.Lines = []api.PurchaseLine{}
		return x, err
	})
	if err != nil || len(list) == 0 {
		return list, err
	}
	ids := make([]uuid.UUID, len(list))
	at := map[uuid.UUID]int{}
	for i, p := range list {
		ids[i], at[p.Id] = p.Id, i
	}
	rows, err = q.Query(ctx, `SELECT l.purchase_id, l.product_id, pr.name, l.area, l.qty_milli, l.unit_price_fils, l.total_fils
		FROM purchase_lines l JOIN products pr ON pr.id = l.product_id
		WHERE l.purchase_id = ANY($1) ORDER BY l.purchase_id, l.position`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid uuid.UUID
		var l api.PurchaseLine
		var qty int64
		if err := rows.Scan(&pid, &l.ProductId, &l.ProductName, &l.Area, &qty, &l.UnitPriceFils, &l.TotalFils); err != nil {
			return nil, err
		}
		l.Quantity = Unmilli(qty)
		list[at[pid]].Lines = append(list[at[pid]].Lines, l)
	}
	return list, rows.Err()
}

func (s *Store) purchase(ctx context.Context, orgID, id uuid.UUID) (api.Purchase, error) {
	var out api.Purchase
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		list, err := s.purchases(ctx, tx, "p.id = $1 AND p.org_id = $2 AND p.deleted_at IS NULL", id, orgID)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return ErrNotFound
		}
		out = list[0]
		return nil
	})
	return out, err
}

// Purchases lists the purchases between two dates, both included, with
// what was bought per area against the area's sales in the same dates.
func (s *Store) Purchases(ctx context.Context, orgID uuid.UUID, from, to time.Time, shopID *uuid.UUID) (api.PurchaseList, error) {
	out := api.PurchaseList{From: from.Format(dateLayout), To: to.Format(dateLayout)}
	if shopID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM shops WHERE id = $1 AND org_id = $2)", *shopID, orgID).
			Scan(&ok); err != nil {
			return out, err
		}
		if !ok {
			return out, &Problem{ErrNotFound, "There's no such shop."}
		}
	}
	args := []any{orgID, out.From, out.To, shopID}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out.Purchases, err = s.purchases(ctx, tx, `p.org_id = $1 AND p.date BETWEEN $2::date AND $3::date
			AND ($4::uuid IS NULL OR p.shop_id = $4) AND p.deleted_at IS NULL`, args...)
		if err != nil {
			return err
		}
		bought := map[api.Area]int64{}
		for _, p := range out.Purchases {
			for _, l := range p.Lines {
				bought[l.Area] += l.TotalFils
			}
			out.TotalFils += p.TotalFils
		}
		// Sales by area: kitchen lines and the rest, less their refunds, on
		// the business days in the dates, as the sales report counts them.
		var kitchen, counter int64
		if err := tx.QueryRow(ctx, `SELECT
				COALESCE((SELECT SUM(l.line_total_fils) FILTER (WHERE l.kitchen) FROM order_lines l
					JOIN orders x ON x.id = l.order_id JOIN business_days d ON d.id = x.business_day_id
					WHERE `+inDays+` AND x.status = 'completed'), 0)::bigint
				- COALESCE((SELECT SUM(rl.amount_fils) FILTER (WHERE l.kitchen) FROM refund_lines rl
					JOIN order_lines l ON l.id = rl.order_line_id JOIN refunds x ON x.id = rl.refund_id
					JOIN business_days d ON d.id = x.business_day_id WHERE `+inDays+`), 0)::bigint,
				COALESCE((SELECT SUM(l.line_total_fils) FILTER (WHERE NOT l.kitchen) FROM order_lines l
					JOIN orders x ON x.id = l.order_id JOIN business_days d ON d.id = x.business_day_id
					WHERE `+inDays+` AND x.status = 'completed'), 0)::bigint
				- COALESCE((SELECT SUM(rl.amount_fils) FILTER (WHERE NOT l.kitchen) FROM refund_lines rl
					JOIN order_lines l ON l.id = rl.order_line_id JOIN refunds x ON x.id = rl.refund_id
					JOIN business_days d ON d.id = x.business_day_id WHERE `+inDays+`), 0)::bigint`, args...).
			Scan(&kitchen, &counter); err != nil {
			return err
		}
		out.SalesFils = kitchen + counter
		for _, a := range Areas {
			as := api.AreaSpend{Area: a, BoughtFils: bought[a]}
			switch a {
			case api.Kitchen:
				as.SalesFils = &kitchen
			case api.Counter:
				as.SalesFils = &counter
			}
			out.Areas = append(out.Areas, as)
		}
		return nil
	})
	return out, err
}
