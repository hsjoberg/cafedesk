package server

import (
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"
	"cafedesk/pkg/money"
)

func (s *Server) UpdateShop(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.ShopInput
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		invalid(w, "name", "Give the shop a name.")
		return
	}
	if !ValidTimezone(in.Timezone) {
		invalid(w, "timezone", "Choose a time zone, such as Asia/Dubai.")
		return
	}
	for _, f := range []struct {
		field, value, name string
		max                int
	}{
		{"name", in.Name, "The name", 80},
		{"tradeName", in.TradeName, "The trade name", 120},
		{"tradeNameAr", in.TradeNameAr, "The trade name in Arabic", 120},
		{"address", in.Address, "The address", 200},
		{"addressAr", in.AddressAr, "The address in Arabic", 200},
		{"phone", in.Phone, "The phone number", 40},
		{"licenceNumber", in.LicenceNumber, "The licence number", 40},
	} {
		if utf8.RuneCountInString(f.value) > f.max {
			invalid(w, f.field, f.name+" is too long for a receipt.")
			return
		}
	}
	sh, err := s.store.UpdateShop(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, store.APIShop(sh))
}

// ValidTimezone reports whether tz names a time zone, such as Asia/Dubai.
func ValidTimezone(tz string) bool {
	tz = strings.TrimSpace(tz)
	if tz == "" || tz == "Local" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

func (s *Server) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	var in api.OrganizationInput
	if !readJSON(w, r, &in) {
		return
	}
	switch {
	case strings.TrimSpace(in.Name) == "":
		invalid(w, "name", "Give the business a name.")
		return
	case utf8.RuneCountInString(in.Name) > 80:
		invalid(w, "name", "The name is too long.")
		return
	case !in.Country.Valid():
		invalid(w, "country", "Choose the UAE or another country.")
		return
	case in.VatPercent < 0 || in.VatPercent > 50 || math.IsNaN(in.VatPercent):
		invalid(w, "vatPercent", "The VAT is a percentage from 0 to 50.")
		return
	}
	if _, ok := money.LookupCurrency(in.Currency); !ok {
		invalid(w, "currency", "Choose a currency from the list.")
		return
	}
	in.VatPercent = math.Round(in.VatPercent*100) / 100
	o, err := s.store.UpdateOrg(r.Context(), user(r).OrgID, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, store.APIOrg(o))
}

func (s *Server) GetVatStatus(w http.ResponseWriter, r *http.Request) {
	orgID := user(r).OrgID
	// Today in the first shop's time, as the reports count business dates.
	loc := time.UTC
	if shops, err := s.store.Shops(r.Context(), orgID); err == nil && len(shops) > 0 {
		if l, err := time.LoadLocation(shops[0].Timezone); err == nil {
			loc = l
		}
	}
	now := s.cfg.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	st, err := s.store.VatStatus(r.Context(), orgID, today)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
