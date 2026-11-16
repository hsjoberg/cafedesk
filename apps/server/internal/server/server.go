// Package server is the HTTP side of the server: the API of
// api/openapi.yaml, the admin's files, and the guards around them.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"
)

// Config is how the server runs.
type Config struct {
	Version string
	// SecureCookies sets the session cookie's Secure flag, except over plain
	// HTTP to a local address (see secureCookie). Off, it is never set.
	SecureCookies bool
	// TrustProxy takes the client's address from X-Forwarded-For, as set by
	// a proxy in front of the server (Railway's). Off, the header is ignored,
	// as anyone could set it.
	TrustProxy bool
	// Admin is the built admin (index.html and its assets), or nil.
	Admin fs.FS
	Log   *slog.Logger
	Now   func() time.Time
}

// Server implements api.ServerInterface.
type Server struct {
	cfg         Config
	store       *store.Store
	logins      *limiter
	pairings    *limiter // per address
	pairingsAll *limiter // in all
}

var _ api.ServerInterface = (*Server)(nil)

// New returns the server's handler: the API, the admin, and their guards.
func New(st *store.Store, cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Server{cfg: cfg, store: st, logins: newLimiter(10, 15*time.Minute),
		pairings: newLimiter(10, 15*time.Minute), pairingsAll: newLimiter(300, 15*time.Minute)}
	mux := http.NewServeMux()
	api.HandlerWithOptions(s, api.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "invalid", err.Error(), "")
		},
	})
	mux.Handle("GET /", s.adminFiles())
	return s.logRequests(securityHeaders(s.sameOrigin(s.authenticate(mux))))
}

// Responses.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message, field string) {
	var e api.Error
	e.Error.Code, e.Error.Message = code, message
	if field != "" {
		e.Error.Field = &field
	}
	writeJSON(w, status, e)
}

// writeStoreError answers for an error from the store.
func (s *Server) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	var p *store.Problem
	switch {
	case errors.As(err, &p) && errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", p.Message, "")
	case errors.As(err, &p):
		writeError(w, http.StatusNotFound, "not_found", p.Message, "")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "That doesn't exist, or was deleted.", "")
	default:
		s.cfg.Log.Error("store", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server. Try again.", "")
	}
}

// readJSON decodes the body into v, rejecting unknown fields and bodies over
// 64 KB.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return readJSONLimit(w, r, v, 64<<10)
}

// readJSONLimit is readJSON for bodies up to limit bytes.
func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "The request isn't valid JSON for this endpoint: "+err.Error(), "")
		return false
	}
	return true
}

// Health and ping.

func (s *Server) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) Ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.Ping{Status: api.Ok, Version: s.cfg.Version, ServerTime: s.cfg.Now().UTC()})
}

// Guards around every request.

// securityHeaders sets headers that keep the admin from being framed,
// sniffed, or loading anything from elsewhere.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin refuses requests that change data unless they are JSON from
// the admin's own origin, which a page on another site can't send with the
// session cookie.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			host := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
			if host != r.Host {
				writeError(w, http.StatusForbidden, "cross_origin", "Requests from other sites aren't allowed.", "")
				return
			}
		} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeError(w, http.StatusForbidden, "cross_origin", "Requests from other sites aren't allowed.", "")
			return
		}
		if r.ContentLength != 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "not_json", "Send JSON, with Content-Type: application/json.", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || (r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/v1/")) {
			return // Railway's health checks, and the admin's files
		}
		s.cfg.Log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}
