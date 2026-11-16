package server

import (
	"net/http"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/auth"
)

func (s *Server) ListStaff(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListStaff(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// validStaff checks a person, and returns the hash of the PIN given, if one
// was. A PIN must be 4 digits, not 1234, and no other active person's.
func (s *Server) validStaff(w http.ResponseWriter, r *http.Request, in *api.StaffInput, id api.ID, pinRequired bool) (*string, bool) {
	if !cleanName(w, "name", "name", &in.Name, true) {
		return nil, false
	}
	switch {
	case in.Role != api.StaffRoleCashier && in.Role != api.StaffRoleManager:
		invalid(w, "role", "Choose cashier or manager.")
		return nil, false
	case len(in.ShopIds) == 0:
		invalid(w, "shopIds", "Choose at least one shop whose tills they use.")
		return nil, false
	}
	if in.Pin == nil || *in.Pin == "" {
		if pinRequired {
			invalid(w, "pin", "Enter a starting PIN of 4 digits.")
			return nil, false
		}
		return nil, true
	}
	pin := *in.Pin
	switch {
	case !auth.ValidPIN(pin):
		invalid(w, "pin", "A PIN is 4 digits.")
		return nil, false
	case pin == "1234":
		invalid(w, "pin", "Choose a PIN other than 1234.")
		return nil, false
	}
	others, err := s.store.PINHashes(r.Context(), user(r).OrgID, id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return nil, false
	}
	for _, h := range others {
		if auth.CheckPIN(h, pin) {
			invalid(w, "pin", "Someone else has that PIN. Choose another.")
			return nil, false
		}
	}
	hash := auth.HashPIN(pin)
	return &hash, true
}

func (s *Server) CreateStaff(w http.ResponseWriter, r *http.Request) {
	var in api.StaffInput
	if !readJSON(w, r, &in) {
		return
	}
	hash, ok := s.validStaff(w, r, &in, api.ID{}, true)
	if !ok {
		return
	}
	st, err := s.store.CreateStaff(r.Context(), user(r).OrgID, in, *hash)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) UpdateStaff(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.StaffInput
	if !readJSON(w, r, &in) {
		return
	}
	hash, ok := s.validStaff(w, r, &in, id, false)
	if !ok {
		return
	}
	st, err := s.store.UpdateStaff(r.Context(), user(r).OrgID, id, in, hash)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
