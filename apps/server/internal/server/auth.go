package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/auth"
	"cafedesk/apps/server/internal/store"
)

const (
	sessionCookie = "cafedesk_session"
	sessionLength = 14 * 24 * time.Hour
)

type ctxKey int

const userKey ctxKey = 0

// user returns the signed-in user of a request that passed authenticate.
func user(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

// public lists what can be reached without signing in.
func public(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/healthz" || p == "/v1/ping":
		return true
	case p == "/v1/admin/session" && r.Method == http.MethodPost:
		return true
	case p == "/v1/terminal/pair" && r.Method == http.MethodPost:
		return true
	case !strings.HasPrefix(p, "/v1/"): // the admin's files
		return true
	}
	return false
}

// authenticate finds the till of a device token, for /v1/terminal/, or the
// session of the request's cookie, for the rest, and refuses requests to
// the API that need one and lack it.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/terminal/") {
			if !public(r) {
				token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
				t, err := s.store.TerminalByToken(r.Context(), auth.HashToken(strings.TrimSpace(token)), s.cfg.Now())
				switch {
				case !ok || errors.Is(err, store.ErrNotFound):
					writeError(w, http.StatusUnauthorized, "unpaired",
						"This till isn't connected, or was disconnected in the admin. Connect it again with a new code.", "")
					return
				case err != nil:
					s.writeStoreError(w, r, err)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), terminalKey, t))
			}
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			u, err := s.store.SessionUser(r.Context(), auth.HashToken(c.Value), s.cfg.Now())
			switch {
			case err == nil:
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
			case !errors.Is(err, store.ErrNotFound):
				s.writeStoreError(w, r, err)
				return
			}
		}
		if !public(r) && user(r).ID == [16]byte{} {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// secureCookie reports whether the session cookie must only travel over
// HTTPS. It must, except for plain HTTP to this PC or to an address of the
// shop's own network, as for a server in the shop, where there is no HTTPS
// and a browser would otherwise drop the cookie. A public address, such as
// Railway's, always gets a secure cookie.
func (s *Server) secureCookie(r *http.Request) bool {
	if !s.cfg.SecureCookies {
		return false
	}
	if r.TLS != nil || (s.cfg.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
		return true
	}
	return !localHost(r.Host)
}

// localHost reports whether host, as a request names the server, is this
// machine or an address of a private network.
func localHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func (s *Server) SignIn(w http.ResponseWriter, r *http.Request) {
	var in api.SignIn
	if !readJSON(w, r, &in) {
		return
	}
	key := s.clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(in.Email))
	if !s.logins.allow(key, s.cfg.Now()) {
		writeError(w, http.StatusTooManyRequests, "too_many_attempts",
			"Too many attempts. Wait 15 minutes, then try again.", "")
		return
	}
	u, hash, err := s.store.UserByEmail(r.Context(), in.Email)
	switch {
	case errors.Is(err, store.ErrNotFound):
		auth.BurnTime(in.Password) // as long as a wrong password takes
	case err != nil:
		s.writeStoreError(w, r, err)
		return
	}
	if err != nil || !auth.VerifyPassword(hash, in.Password) {
		writeError(w, http.StatusUnauthorized, "wrong_credentials", "That email and password don't match.", "")
		return
	}
	s.logins.reset(key)
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.store.CreateSession(r.Context(), u.ID, tokenHash, s.cfg.Now().Add(sessionLength), r.UserAgent()); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, token, int(sessionLength.Seconds()))
	s.writeMe(w, r, u)
}

func (s *Server) SignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			s.writeStoreError(w, r, err)
			return
		}
	}
	s.setSessionCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) { s.writeMe(w, r, user(r)) }

func (s *Server) writeMe(w http.ResponseWriter, r *http.Request, u store.User) {
	org, err := s.store.Org(r.Context(), u.OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	shops, err := s.store.Shops(r.Context(), u.OrgID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	me := api.Me{
		User:         api.User{Id: u.ID, Name: u.Name, Email: u.Email, Role: api.UserRole(u.Role)},
		Organization: api.Organization{Id: org.ID, Name: org.Name},
		Shops:        make([]api.Shop, 0, len(shops)),
	}
	for _, sh := range shops {
		me.Shops = append(me.Shops, api.Shop{Id: sh.ID, Name: sh.Name, Currency: sh.Currency, Timezone: sh.Timezone})
	}
	writeJSON(w, http.StatusOK, me)
}

// clientIP is the request's address, or with TrustProxy the first address
// of X-Forwarded-For.
func (s *Server) clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" && s.cfg.TrustProxy {
		return strings.TrimSpace(strings.Split(f, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter allows max attempts per key in a window, as sign-ins per address
// and email.
type limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, attempts: map[string][]time.Time{}}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	var recent []time.Time
	for _, t := range l.attempts[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.attempts[key] = recent
		return false
	}
	l.attempts[key] = append(recent, now)
	if len(l.attempts) > 10_000 { // forget the oldest keys rather than grow without end
		for k := range l.attempts {
			delete(l.attempts, k)
			if len(l.attempts) <= 5_000 {
				break
			}
		}
	}
	return true
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
