package server

import (
	"net/http"
	"time"

	"cafedesk/apps/server/internal/api"
)

// maxReportDays is the longest range a report covers: a year, leap or not.
const maxReportDays = 366

func (s *Server) GetSalesReport(w http.ResponseWriter, r *http.Request, params api.GetSalesReportParams) {
	from, err := time.Parse("2006-01-02", params.From)
	if err != nil {
		invalid(w, "from", "from is a date, YYYY-MM-DD.")
		return
	}
	to, err := time.Parse("2006-01-02", params.To)
	if err != nil {
		invalid(w, "to", "to is a date, YYYY-MM-DD.")
		return
	}
	switch days := int(to.Sub(from).Hours()/24) + 1; {
	case days < 1:
		invalid(w, "to", "The end date is before the start date.")
		return
	case days > maxReportDays:
		invalid(w, "to", "Choose at most a year at a time.")
		return
	}
	rep, err := s.store.SalesReport(r.Context(), user(r).OrgID, from, to, params.ShopId)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
