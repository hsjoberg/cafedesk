package store

import (
	"context"
	"strings"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ShopDetails is what a shop's receipts print at the top.
type ShopDetails struct {
	TradeName     string
	TradeNameAr   string
	Address       string
	AddressAr     string
	Phone         string
	LicenceNumber string
}

const shopSelect = `SELECT id, name, currency, timezone, trade_name, trade_name_ar, address, address_ar, phone,
	licence_number FROM shops`

func scanShop(r pgx.CollectableRow) (Shop, error) {
	var sh Shop
	d := &sh.Details
	err := r.Scan(&sh.ID, &sh.Name, &sh.Currency, &sh.Timezone, &d.TradeName, &d.TradeNameAr, &d.Address,
		&d.AddressAr, &d.Phone, &d.LicenceNumber)
	return sh, err
}

// Shops lists an organization's shops, oldest first.
func (s *Store) Shops(ctx context.Context, orgID uuid.UUID) ([]Shop, error) {
	rows, err := s.pool.Query(ctx, shopSelect+" WHERE org_id = $1 ORDER BY created_at", orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanShop)
}

// UpdateShop changes a shop's name and the details its receipts print.
func (s *Store) UpdateShop(ctx context.Context, orgID, id uuid.UUID, in api.ShopInput) (Shop, error) {
	t := strings.TrimSpace
	rows, err := s.pool.Query(ctx, `UPDATE shops SET name = $3, trade_name = $4, trade_name_ar = $5, address = $6,
		address_ar = $7, phone = $8, licence_number = $9 WHERE id = $1 AND org_id = $2
		RETURNING id, name, currency, timezone, trade_name, trade_name_ar, address, address_ar, phone, licence_number`,
		id, orgID, t(in.Name), t(in.TradeName), t(in.TradeNameAr), t(in.Address), t(in.AddressAr), t(in.Phone),
		t(in.LicenceNumber))
	if err != nil {
		return Shop{}, err
	}
	sh, err := pgx.CollectExactlyOneRow(rows, scanShop)
	if err == pgx.ErrNoRows {
		return sh, ErrNotFound
	}
	return sh, err
}

// APIShop is a shop as the API shows it.
func APIShop(sh Shop) api.Shop {
	d := sh.Details
	return api.Shop{Id: sh.ID, Name: sh.Name, Currency: sh.Currency, Timezone: sh.Timezone,
		TradeName: d.TradeName, TradeNameAr: d.TradeNameAr, Address: d.Address, AddressAr: d.AddressAr,
		Phone: d.Phone, LicenceNumber: d.LicenceNumber}
}

// receiptHeader is what a shop's tills print at the top of receipts.
func receiptHeader(ctx context.Context, tx pgx.Tx, shopID uuid.UUID) (api.ReceiptHeader, error) {
	var h api.ReceiptHeader
	err := tx.QueryRow(ctx, `SELECT trade_name, trade_name_ar, address, address_ar, phone, licence_number
		FROM shops WHERE id = $1`, shopID).
		Scan(&h.TradeName, &h.TradeNameAr, &h.Address, &h.AddressAr, &h.Phone, &h.LicenceNumber)
	return h, err
}

// The UAE's VAT registration thresholds, over 12 months.
const (
	vatMandatoryFils = 375_000_00
	vatVoluntaryFils = 187_500_00
)

// VatStatus sums an organization's net sales, every shop's, over the 12
// months up to today, against the VAT registration thresholds.
func (s *Store) VatStatus(ctx context.Context, orgID uuid.UUID, today time.Time) (api.VatStatus, error) {
	to := today
	from := to.AddDate(-1, 0, 1)
	st := api.VatStatus{From: from.Format(dateLayout), To: to.Format(dateLayout),
		MandatoryFils: vatMandatoryFils, VoluntaryFils: vatVoluntaryFils}
	args := []any{orgID, st.From, st.To, nil}
	err := s.pool.QueryRow(ctx, `SELECT
			COALESCE((SELECT SUM(x.total_fils) FROM orders x JOIN business_days d ON d.id = x.business_day_id
				WHERE x.status = 'completed' AND `+inDays+`), 0)::bigint
			- COALESCE((SELECT SUM(x.total_fils) FROM refunds x JOIN business_days d ON d.id = x.business_day_id
				WHERE `+inDays+`), 0)::bigint,
			EXISTS (SELECT 1 FROM shops WHERE org_id = $1 AND tax_registered)`, args...).
		Scan(&st.SalesFils, &st.Registered)
	return st, err
}
