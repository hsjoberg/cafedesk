// Package store reads and writes the server's database. Every query is
// scoped to an organization ID that comes from the caller's session or
// device token, never from the request body.
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound is returned for a row that doesn't exist in the
	// organization, whether or not it exists in another.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned for a change the data doesn't allow.
	ErrConflict = errors.New("conflict")
)

// Problem is an error a person can act on: its message is shown to them as
// it is. Kind is ErrNotFound or ErrConflict.
type Problem struct {
	Kind    error
	Message string
}

func (p *Problem) Error() string { return p.Message }
func (p *Problem) Unwrap() error { return p.Kind }

// Store is the database.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a store on the pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// inTx runs fn in a transaction, committing if it returns nil.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, fn)
}

// User is someone who signs in to the admin.
type User struct {
	ID     uuid.UUID
	OrgID  uuid.UUID
	Email  string
	Name   string
	Role   string // "owner" or "manager"
	Active bool
}

// Org is an organization.
type Org struct {
	ID   uuid.UUID
	Name string
}

// Shop is a shop of an organization.
type Shop struct {
	ID       uuid.UUID
	Name     string
	Currency string
	Timezone string
}

// Setup creates the first organization, its first shop, and its owner. It
// fails with ErrConflict if an organization already exists: it is for a
// new server.
func (s *Store) Setup(ctx context.Context, orgName, shopName, ownerName, email, passwordHash string) (Org, Shop, User, error) {
	org := Org{ID: newID(), Name: strings.TrimSpace(orgName)}
	shop := Shop{ID: newID(), Name: strings.TrimSpace(shopName), Currency: "AED", Timezone: "Asia/Dubai"}
	user := User{ID: newID(), OrgID: org.ID, Email: normalEmail(email), Name: strings.TrimSpace(ownerName),
		Role: "owner", Active: true}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM organizations").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, "INSERT INTO organizations (id, name) VALUES ($1, $2)", org.ID, org.Name); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO shops (id, org_id, name, currency, timezone) VALUES ($1, $2, $3, $4, $5)",
			shop.ID, org.ID, shop.Name, shop.Currency, shop.Timezone); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO users (id, org_id, email, name, role, password_hash)
			VALUES ($1, $2, $3, $4, $5, $6)`, user.ID, org.ID, user.Email, user.Name, user.Role, passwordHash)
		return err
	})
	return org, shop, user, err
}

func normalEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// IsSetUp reports whether the server has an organization yet.
func (s *Store) IsSetUp(ctx context.Context) (bool, error) {
	var done bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM organizations)").Scan(&done)
	return done, err
}

// UserByEmail returns an active user and their password hash.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT id, org_id, email, name, role, active, password_hash FROM users
		WHERE lower(email) = $1 AND active`, normalEmail(email)).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role, &u.Active, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, "", ErrNotFound
	}
	return u, hash, err
}

// CreateSession records a session by the hash of its token.
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, expires time.Time, userAgent string) error {
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at, user_agent)
		VALUES ($1, $2, $3, $4)`, tokenHash, userID, expires, userAgent)
	return err
}

// SessionUser returns the active user of an unexpired session, and notes
// that the session was used.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte, now time.Time) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.org_id, u.email, u.name, u.role, u.active
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND u.active`, tokenHash, now).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role, &u.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2
		WHERE token_hash = $1 AND last_seen_at < $2::timestamptz - interval '1 minute'`, tokenHash, now)
	return u, err
}

// DeleteSession ends a session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
	return err
}

// DeleteExpiredSessions removes sessions past their expiry.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= $1", now)
	return err
}

// Org returns an organization.
func (s *Store) Org(ctx context.Context, id uuid.UUID) (Org, error) {
	o := Org{ID: id}
	err := s.pool.QueryRow(ctx, "SELECT name FROM organizations WHERE id = $1", id).Scan(&o.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

// Shops lists an organization's shops.
func (s *Store) Shops(ctx context.Context, orgID uuid.UUID) ([]Shop, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, name, currency, timezone FROM shops WHERE org_id = $1 ORDER BY created_at", orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Shop, error) {
		var sh Shop
		err := r.Scan(&sh.ID, &sh.Name, &sh.Currency, &sh.Timezone)
		return sh, err
	})
}
