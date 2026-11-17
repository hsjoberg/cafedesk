package server

import (
	"net/http"
	"time"

	"cafedesk/apps/server/internal/api"
)

// maxReportDays is the longest range a report covers: a year, leap or not.
const maxReportDays = 366

func (s *Server) GetSalesReport(w http.ResponseWriter, r *http.Request, params api.GetSalesReportParams) {
	from, to, ok := reportRange(w, params.From, params.To)
	if !ok {
		return
	}
	rep, err := s.store.SalesReport(r.Context(), user(r).OrgID, from, to, params.ShopId)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) GetOrders(w http.ResponseWriter, r *http.Request, params api.GetOrdersParams) {
	from, to, ok := reportRange(w, params.From, params.To)
	if !ok {
		return
	}
	if to.Sub(from) > 30*24*time.Hour {
		invalid(w, "to", "Choose at most 31 days of orders at a time.")
		return
	}
	list, err := s.store.Orders(r.Context(), user(r).OrgID, from, to, params.ShopId)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
