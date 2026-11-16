package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/auth"
	"cafedesk/apps/server/internal/store"
)

// Tills: the admin's side (add, new code, unpair), and the till's (pair,
// pull changes, send events).

const terminalKey ctxKey = 1

// terminal returns the paired till of a request that passed authenticate.
func terminal(r *http.Request) store.Terminal {
	t, _ := r.Context().Value(terminalKey).(store.Terminal)
	return t
}

// pairingCode returns 8 random digits and the hash the database keeps.
func pairingCode() (string, []byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return "", nil, err
	}
	code := fmt.Sprintf("%08d", n.Int64())
	return code, auth.HashToken(code), nil
}

func (s *Server) ListTerminals(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTerminals(r.Context(), user(r).OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) CreateTerminal(w http.ResponseWriter, r *http.Request) {
	var in api.TerminalInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Name != nil && !cleanName(w, "name", "till's name", in.Name, false) {
		return
	}
	code, hash, err := pairingCode()
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	p, err := s.store.CreateTerminal(r.Context(), user(r).OrgID, in, hash, s.cfg.Now())
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	p.Code = code
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) CreatePairingCode(w http.ResponseWriter, r *http.Request, id api.ID) {
	code, hash, err := pairingCode()
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	p, err := s.store.NewPairingCode(r.Context(), user(r).OrgID, id, hash, s.cfg.Now())
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	p.Code = code
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) UnpairTerminal(w http.ResponseWriter, r *http.Request, id api.ID) {
	if err := s.store.UnpairTerminal(r.Context(), user(r).OrgID, id, s.cfg.Now()); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) PairTerminal(w http.ResponseWriter, r *http.Request) {
	var in api.PairRequest
	if !readJSON(w, r, &in) {
		return
	}
	// Ten tries per address, and 300 in all, in 15 minutes: guessing one of
	// 100 million codes in the ten minutes one lasts is hopeless.
	now := s.cfg.Now()
	if !s.pairings.allow(s.clientIP(r), now) || !s.pairingsAll.allow("all", now) {
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "Too many attempts. Wait 15 minutes, then try again.", "")
		return
	}
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, in.Code)
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	p, err := s.store.Pair(r.Context(), auth.HashToken(code), tokenHash, now)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "bad_code",
			"That code doesn't work. A code lasts 10 minutes and works once: show a new one in the admin, on Tills.", "code")
		return
	}
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	p.Token = token
	s.cfg.Log.Info("paired", "terminal", p.Terminal.Id, "shop", p.Shop.Id)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) GetChanges(w http.ResponseWriter, r *http.Request, params api.GetChangesParams) {
	if params.Since < 0 {
		invalid(w, "since", "since is a version, 0 or more.")
		return
	}
	ch, err := s.store.Changes(r.Context(), terminal(r), params.Since)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) SendEvents(w http.ResponseWriter, r *http.Request) {
	var in api.EventBatch
	if !readJSONLimit(w, r, &in, 4<<20) {
		return
	}
	if len(in.Events) > 200 {
		invalid(w, "events", "Send at most 200 events at a time.")
		return
	}
	t := terminal(r)
	res := api.EventResult{Accepted: []int64{}}
	for _, ev := range in.Events {
		err := s.store.ApplyEvent(r.Context(), t, ev)
		if errors.Is(err, store.ErrRejected) {
			msg := strings.TrimPrefix(err.Error(), store.ErrRejected.Error()+": ")
			res.Rejected = &api.EventRejection{Seq: ev.Seq, Message: msg}
			s.cfg.Log.Warn("event rejected", "terminal", t.ID, "seq", ev.Seq, "type", ev.Type, "why", msg)
			break
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				break
			}
			s.cfg.Log.Error("event", "terminal", t.ID, "seq", ev.Seq, "err", err)
			res.Rejected = &api.EventRejection{Seq: ev.Seq, Message: "The server couldn't save it. It will be tried again."}
			break
		}
		res.Accepted = append(res.Accepted, ev.Seq)
	}
	writeJSON(w, http.StatusOK, res)
}
