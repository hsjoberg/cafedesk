package server

import (
	"fmt"
	"math"
	"net/http"
	"slices"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"
)

var units = []api.Unit{api.G, api.Ml, api.Piece}

func (s *Server) ListIngredients(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListIngredients(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func validIngredient(w http.ResponseWriter, in *api.IngredientInput) bool {
	if !cleanName(w, "name", "ingredient's name", &in.Name, true) {
		return false
	}
	if !slices.Contains(units, in.Unit) {
		invalid(w, "unit", "Choose grams, millilitres or pieces.")
		return false
	}
	return true
}

func (s *Server) CreateIngredient(w http.ResponseWriter, r *http.Request) {
	var in api.IngredientInput
	if !readJSON(w, r, &in) || !validIngredient(w, &in) {
		return
	}
	x, err := s.store.CreateIngredient(r.Context(), user(r).OrgID, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) UpdateIngredient(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.IngredientInput
	if !readJSON(w, r, &in) || !validIngredient(w, &in) {
		return
	}
	x, err := s.store.UpdateIngredient(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) DeleteIngredient(w http.ResponseWriter, r *http.Request, id api.ID) {
	if err := s.store.DeleteIngredient(r.Context(), user(r).OrgID, id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) UpdateProduct(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.ProductInput
	if !readJSON(w, r, &in) || !cleanName(w, "name", "product", &in.Name, true) {
		return
	}
	if !slices.Contains(store.Areas, in.Area) {
		invalid(w, "area", "Choose where it goes: counter, kitchen, packaging or other.")
		return
	}
	if p := in.PackAmount; p != nil {
		if *p <= 0 || *p > 10_000_000 || math.IsNaN(*p) || store.Milli(*p) == 0 {
			invalid(w, "packAmount", "Enter how much one holds, more than 0.")
			return
		}
		if math.Abs(*p*1000-math.Round(*p*1000)) > 1e-6 {
			invalid(w, "packAmount", "Use at most 3 decimals.")
			return
		}
	}
	if in.PackUom != nil && !store.ValidUom(store.NormalUom(*in.PackUom)) {
		invalid(w, "packUom", "A unit is up to 8 letters, as EA, CAR or PAC.")
		return
	}
	if in.Packs != nil {
		seen := map[string]bool{}
		for i, p := range *in.Packs {
			u := store.NormalUom(p.Uom)
			field := fmt.Sprintf("packs.%d", i)
			if u == "" || !store.ValidUom(u) {
				invalid(w, field+".uom", "A unit is up to 8 letters, as EA, CAR or PAC.")
				return
			}
			if seen[u] {
				invalid(w, field+".uom", "Each unit once.")
				return
			}
			seen[u] = true
			if p.PackAmount <= 0 || p.PackAmount > 10_000_000 || math.IsNaN(p.PackAmount) ||
				math.Abs(p.PackAmount*1000-math.Round(p.PackAmount*1000)) > 1e-6 {
				invalid(w, field+".packAmount", "Enter how much one holds, more than 0, with at most 3 decimals.")
				return
			}
		}
	}
	x, err := s.store.UpdateProduct(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}
