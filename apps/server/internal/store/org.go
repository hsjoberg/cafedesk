package store

import (
	"context"
	"errors"
	"strings"

	"cafedesk/apps/server/internal/api"
	"cafedesk/pkg/money"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// APIOrg is an organization as the API shows it.
func APIOrg(o Org) api.Organization {
	return api.Organization{Id: o.ID, Name: o.Name, Currency: o.Currency, CurrencyDecimals: currencyOf(o.Currency).Decimals,
		Country: api.Country(o.Country), VatPercent: o.VatPercent}
}

// currencyOf is a currency by its code; dirhams for one the server doesn't
// know, which can't be stored.
func currencyOf(code string) money.Currency {
	if c, ok := money.LookupCurrency(code); ok {
		return c
	}
	return money.AED
}

// UpdateOrg changes an organization's name and settings. The currency can
// change only while nothing is recorded in it.
func (s *Store) UpdateOrg(ctx context.Context, id uuid.UUID, in api.OrganizationInput) (Org, error) {
	cur := currencyOf(in.Currency).Code
	o := Org{ID: id, Name: strings.TrimSpace(in.Name), Currency: cur, Country: string(in.Country), VatPercent: in.VatPercent}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var was string
		if err := tx.QueryRow(ctx, "SELECT currency FROM organizations WHERE id = $1 FOR UPDATE", id).Scan(&was); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if was != cur {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE org_id = $1)
				OR EXISTS (SELECT 1 FROM purchases WHERE org_id = $1)
				OR EXISTS (SELECT 1 FROM items WHERE org_id = $1 AND price_fils > 0)`, id).Scan(&used); err != nil {
				return err
			}
			if used {
				return &Problem{ErrConflict, "The currency can't change once there are prices, sales or purchases in it."}
			}
		}
		_, err := tx.Exec(ctx, `UPDATE organizations SET name = $2, currency = $3, country = $4, vat_percent = $5
			WHERE id = $1`, id, o.Name, o.Currency, o.Country, o.VatPercent)
		return err
	})
	return o, err
}
