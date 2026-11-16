package server

import (
	"fmt"
	"math"
	"net/http"

	"cafedesk/apps/server/internal/api"
)

const maxRecipeLines = 60

// validLines checks a recipe's lines: each an ingredient or a batch, with an
// amount. Below 0 only where negative is allowed (an option's: "No sugar");
// batches only where they may be (not in a batch).
func validLines(w http.ResponseWriter, lines []api.RecipeLine, negative, batches bool) bool {
	if len(lines) > maxRecipeLines {
		invalid(w, "lines", fmt.Sprintf("A recipe can have up to %d lines.", maxRecipeLines))
		return false
	}
	for i, l := range lines {
		field := fmt.Sprintf("lines.%d", i)
		switch {
		case (l.IngredientId == nil) == (l.BatchId == nil):
			invalid(w, field, "Each line is an ingredient or a batch.")
			return false
		case l.BatchId != nil && !batches:
			invalid(w, field+".batchId", "A batch is made of ingredients, not of other batches.")
			return false
		}
		a := l.Amount
		if math.IsNaN(a) || a == 0 || math.Abs(a) > 10_000_000 || math.Abs(a*1000-math.Round(a*1000)) > 1e-6 {
			invalid(w, field+".amount", "Enter how much, with at most 3 decimals.")
			return false
		}
		if a < 0 && !negative {
			invalid(w, field+".amount", "Enter how much, more than 0.")
			return false
		}
	}
	return true
}

func (s *Server) GetRecipes(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.Recipes(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) SetItemRecipe(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.RecipeInput
	if !readJSON(w, r, &in) || !validLines(w, in.Lines, false, true) {
		return
	}
	out, err := s.store.SetItemRecipe(r.Context(), user(r).OrgID, id, in.Lines)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) SetModifierRecipe(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.RecipeInput
	if !readJSON(w, r, &in) || !validLines(w, in.Lines, true, true) {
		return
	}
	out, err := s.store.SetModifierRecipe(r.Context(), user(r).OrgID, id, in.Lines)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func validBatch(w http.ResponseWriter, in *api.BatchInput) bool {
	if !cleanName(w, "name", "batch's name", &in.Name, true) {
		return false
	}
	y := in.YieldPortions
	if math.IsNaN(y) || y <= 0 || y > 100_000 || math.Abs(y*1000-math.Round(y*1000)) > 1e-6 {
		invalid(w, "yieldPortions", "Enter how many portions it makes, more than 0.")
		return false
	}
	return validLines(w, in.Lines, false, false)
}

func (s *Server) CreateBatch(w http.ResponseWriter, r *http.Request) {
	var in api.BatchInput
	if !readJSON(w, r, &in) || !validBatch(w, &in) {
		return
	}
	out, err := s.store.CreateBatch(r.Context(), user(r).OrgID, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) UpdateBatch(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.BatchInput
	if !readJSON(w, r, &in) || !validBatch(w, &in) {
		return
	}
	out, err := s.store.UpdateBatch(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) DeleteBatch(w http.ResponseWriter, r *http.Request, id api.ID) {
	if err := s.store.DeleteBatch(r.Context(), user(r).OrgID, id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
