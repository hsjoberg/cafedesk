package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"cafedesk/apps/server/internal/api"
)

const (
	maxName   = 60
	maxPrice  = 1_000_000 // fils: AED 10,000
	maxDelta  = 100_000   // fils: AED 1,000 either way
	maxGroups = 10
	maxMods   = 30
)

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// invalid answers 400 for a field.
func invalid(w http.ResponseWriter, field, message string) {
	writeError(w, http.StatusBadRequest, "invalid", message, field)
}

// cleanName trims a name and checks its length; what is a person's name for
// the thing, as "item name", goes into the message.
func cleanName(w http.ResponseWriter, field, what string, s *string, required bool) bool {
	*s = strings.TrimSpace(*s)
	switch {
	case required && *s == "":
		invalid(w, field, "Enter the "+what+".")
		return false
	case utf8.RuneCountInString(*s) > maxName:
		invalid(w, field, fmt.Sprintf("Keep the %s to %d characters.", what, maxName))
		return false
	}
	return true
}

func cleanOptional(w http.ResponseWriter, field, what string, s **string) bool {
	if *s == nil {
		return true
	}
	return cleanName(w, field, what, *s, false)
}

func (s *Server) GetCatalog(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.Catalog(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// Categories.

func validCategory(w http.ResponseWriter, in *api.CategoryInput) bool {
	if !cleanName(w, "name", "category name", &in.Name, true) || !cleanOptional(w, "nameAr", "Arabic name", &in.NameAr) {
		return false
	}
	if !hexColor.MatchString(in.Color) {
		invalid(w, "color", "Choose a color.")
		return false
	}
	in.Color = strings.ToUpper(in.Color)
	return true
}

func (s *Server) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var in api.CategoryInput
	if !readJSON(w, r, &in) || !validCategory(w, &in) {
		return
	}
	c, err := s.store.CreateCategory(r.Context(), user(r).OrgID, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) UpdateCategory(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.CategoryInput
	if !readJSON(w, r, &in) || !validCategory(w, &in) {
		return
	}
	c, err := s.store.UpdateCategory(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) DeleteCategory(w http.ResponseWriter, r *http.Request, id api.ID) {
	if err := s.store.DeleteCategory(r.Context(), user(r).OrgID, id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) MoveCategory(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.Move
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.store.MoveCategory(r.Context(), user(r).OrgID, id, in.Position); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Items.

func validItem(w http.ResponseWriter, in *api.ItemInput) bool {
	if !cleanName(w, "name", "item name", &in.Name, true) || !cleanOptional(w, "nameAr", "Arabic name", &in.NameAr) {
		return false
	}
	switch {
	case in.PriceFils < 0 || in.PriceFils > maxPrice:
		invalid(w, "priceFils", "Enter a price from 0.00 to 10,000.00.")
		return false
	case len(in.ModifierGroupIds) > maxGroups:
		invalid(w, "modifierGroupIds", fmt.Sprintf("An item can have up to %d option groups.", maxGroups))
		return false
	}
	return true
}

func (s *Server) CreateItem(w http.ResponseWriter, r *http.Request) {
	var in api.ItemInput
	if !readJSON(w, r, &in) || !validItem(w, &in) {
		return
	}
	it, err := s.store.CreateItem(r.Context(), user(r).OrgID, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) UpdateItem(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.ItemInput
	if !readJSON(w, r, &in) || !validItem(w, &in) {
		return
	}
	it, err := s.store.UpdateItem(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) DeleteItem(w http.ResponseWriter, r *http.Request, id api.ID) {
	if err := s.store.DeleteItem(r.Context(), user(r).OrgID, id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) MoveItem(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.Move
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.store.MoveItem(r.Context(), user(r).OrgID, id, in.Position); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Option groups.

func validGroup(w http.ResponseWriter, in *api.ModifierGroupInput) bool {
	if !cleanName(w, "name", "group name", &in.Name, true) || !cleanOptional(w, "nameAr", "Arabic name", &in.NameAr) {
		return false
	}
	switch {
	case len(in.Modifiers) == 0:
		invalid(w, "modifiers", "Add at least one option.")
		return false
	case len(in.Modifiers) > maxMods:
		invalid(w, "modifiers", fmt.Sprintf("A group can have up to %d options.", maxMods))
		return false
	}
	defaults := 0
	for i := range in.Modifiers {
		m := &in.Modifiers[i]
		field := fmt.Sprintf("modifiers.%d", i)
		if !cleanName(w, field+".name", "option name", &m.Name, true) ||
			!cleanOptional(w, field+".nameAr", "Arabic name", &m.NameAr) {
			return false
		}
		if m.PriceDeltaFils < -maxDelta || m.PriceDeltaFils > maxDelta {
			invalid(w, field+".priceDeltaFils", "Keep a price change within 1,000.00 either way.")
			return false
		}
		if m.IsDefault {
			defaults++
		}
	}
	if !in.Multiple && defaults > 1 {
		invalid(w, "modifiers", "A group where one option is chosen can have only one default.")
		return false
	}
	return true
}

func (s *Server) CreateModifierGroup(w http.ResponseWriter, r *http.Request) {
	var in api.ModifierGroupInput
	if !readJSON(w, r, &in) || !validGroup(w, &in) {
		return
	}
	g, err := s.store.CreateModifierGroup(r.Context(), user(r).OrgID, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) UpdateModifierGroup(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.ModifierGroupInput
	if !readJSON(w, r, &in) || !validGroup(w, &in) {
		return
	}
	g, err := s.store.UpdateModifierGroup(r.Context(), user(r).OrgID, id, in)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) DeleteModifierGroup(w http.ResponseWriter, r *http.Request, id api.ID) {
	if err := s.store.DeleteModifierGroup(r.Context(), user(r).OrgID, id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) MoveModifierGroup(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.Move
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.store.MoveModifierGroup(r.Context(), user(r).OrgID, id, in.Position); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
