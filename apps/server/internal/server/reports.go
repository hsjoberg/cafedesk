package server

import (
	"net/http"

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
