package server

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"
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
