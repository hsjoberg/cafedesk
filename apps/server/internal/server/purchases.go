package server

import (
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"
)

const (
	maxLines      = 200
	maxQuantity   = 1_000_000
	maxLineAmount = 100_000_000 // 1,000,000.00
)

// reportRange checks a report's dates: valid, in order, and at most a year
// apart.
func reportRange(w http.ResponseWriter, fromS, toS string) (from, to time.Time, ok bool) {
	from, err := time.Parse("2006-01-02", fromS)
	if err != nil {
		invalid(w, "from", "from is a date, YYYY-MM-DD.")
		return from, to, false
	}
	to, err = time.Parse("2006-01-02", toS)
	if err != nil {
		invalid(w, "to", "to is a date, YYYY-MM-DD.")
		return from, to, false
	}
	switch days := int(to.Sub(from).Hours()/24) + 1; {
	case days < 1:
		invalid(w, "to", "The end date is before the start date.")
		return from, to, false
	case days > maxReportDays:
		invalid(w, "to", "Choose at most a year at a time.")
		return from, to, false
	}
	return from, to, true
}

func (s *Server) ListPurchases(w http.ResponseWriter, r *http.Request, params api.ListPurchasesParams) {
	from, to, ok := reportRange(w, params.From, params.To)
	if !ok {
		return
	}
	list, err := s.store.Purchases(r.Context(), user(r).OrgID, from, to, params.ShopId)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// validPurchase checks a purchase and tidies its names.
func validPurchase(w http.ResponseWriter, in *api.PurchaseInput) bool {
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		invalid(w, "date", "Enter the date it was bought.")
		return false
	}
	if in.SupplierId == nil && in.SupplierName != nil && !cleanName(w, "supplierName", "supplier's name", in.SupplierName, false) {
		return false
	}
	if in.Reference != nil {
		ref := strings.TrimSpace(*in.Reference)
		if len([]rune(ref)) > 60 {
			invalid(w, "reference", "Keep the invoice number to 60 characters.")
			return false
		}
		in.Reference = &ref
	}
	switch {
	case len(in.Lines) == 0:
		invalid(w, "lines", "Add at least one line.")
		return false
	case len(in.Lines) > maxLines:
		invalid(w, "lines", fmt.Sprintf("A purchase can have up to %d lines.", maxLines))
		return false
	}
	for i := range in.Lines {
		l := &in.Lines[i]
		field := fmt.Sprintf("lines.%d", i)
		if l.ProductId == nil {
			if l.ProductName == nil {
				l.ProductName = new(string)
			}
			if !cleanName(w, field+".productName", "product", l.ProductName, true) {
				return false
			}
		}
		if !slices.Contains(store.Areas, l.Area) {
			invalid(w, field+".area", "Choose where it goes: counter, kitchen, packaging or other.")
			return false
		}
		q := l.Quantity
		if q <= 0 || q > maxQuantity || math.IsNaN(q) || store.Milli(q) == 0 {
			invalid(w, field+".quantity", "Enter how many, more than 0.")
			return false
		}
		if math.Abs(q*1000-math.Round(q*1000)) > 1e-6 {
			invalid(w, field+".quantity", "Use at most 3 decimals, as 1.25.")
			return false
		}
		if l.UnitPriceFils < 0 || l.UnitPriceFils > maxLineAmount {
			invalid(w, field+".unitPriceFils", "Enter a price from 0.00 to 1,000,000.00.")
			return false
		}
		if l.TotalFils < 0 || l.TotalFils > maxLineAmount {
			invalid(w, field+".totalFils", "Enter an amount from 0.00 to 1,000,000.00.")
			return false
		}
		if l.VatFils != nil && (*l.VatFils < 0 || *l.VatFils > l.TotalFils) {
			invalid(w, field+".vatFils", "The VAT is part of the amount: from 0 up to it.")
			return false
		}
		if l.ProductCode != nil {
			code := strings.TrimSpace(*l.ProductCode)
			if len([]rune(code)) > 40 {
				invalid(w, field+".productCode", "Keep a code to 40 characters.")
				return false
			}
			l.ProductCode = &code
		}
		if p := l.PackAmount; p != nil && (*p <= 0 || *p > 10_000_000 || math.IsNaN(*p) ||
			math.Abs(*p*1000-math.Round(*p*1000)) > 1e-6) {
			invalid(w, field+".packAmount", "Enter how much one holds, more than 0, with at most 3 decimals.")
			return false
		}
	}
	return true
}

func (s *Server) CreatePurchase(w http.ResponseWriter, r *http.Request) {
	var in api.PurchaseInput
	if !readJSONLimit(w, r, &in, 1<<20) || !validPurchase(w, &in) {
		return
	}
	u := user(r)
	p, err := s.store.CreatePurchase(r.Context(), u.OrgID, u.ID, in, s.cfg.Now())
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) UpdatePurchase(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.PurchaseInput
	if !readJSONLimit(w, r, &in, 1<<20) || !validPurchase(w, &in) {
		return
	}
	u := user(r)
	p, err := s.store.UpdatePurchase(r.Context(), u.OrgID, u.ID, id, in, s.cfg.Now())
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) DeletePurchase(w http.ResponseWriter, r *http.Request, id api.ID) {
	u := user(r)
	if err := s.store.DeletePurchase(r.Context(), u.OrgID, u.ID, id, s.cfg.Now()); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListSuppliers(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSuppliers(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) ListProducts(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProducts(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) UpdateSupplier(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.SupplierInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.QuietUntil != nil {
		if _, err := time.Parse("2006-01-02", *in.QuietUntil); err != nil {
			invalid(w, "quietUntil", "quietUntil is a date, YYYY-MM-DD.")
			return
		}
	}
	x, err := s.store.UpdateSupplier(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) GetUsage(w http.ResponseWriter, r *http.Request, params api.GetUsageParams) {
	from, to, ok := reportRange(w, params.From, params.To)
	if !ok {
		return
	}
	out, err := s.store.Usage(r.Context(), user(r).OrgID, from, to, params.ShopId)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
