package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PairingLifetime is how long a pairing code works.
const PairingLifetime = 10 * time.Minute

// Terminal is a paired till, as its device token identifies it.
type Terminal struct {
	ID     uuid.UUID
	OrgID  uuid.UUID
	ShopID uuid.UUID
	Code   string
}

const terminalCols = `t.id, t.shop_id, t.code, t.name,
	CASE WHEN t.revoked_at IS NOT NULL THEN 'unpaired' WHEN t.paired_at IS NULL THEN 'waiting' ELSE 'paired' END,
	t.paired_at, t.last_seen_at`

func scanTerminal(r pgx.Row) (api.Terminal, error) {
	var t api.Terminal
	var status string
	err := r.Scan(&t.Id, &t.ShopId, &t.Code, &t.Name, &status, &t.PairedAt, &t.LastSeenAt)
	t.Status = api.TerminalStatus(status)
	return t, err
}

// ListTerminals lists the organization's tills.
func (s *Store) ListTerminals(ctx context.Context, orgID uuid.UUID) ([]api.Terminal, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+terminalCols+` FROM terminals t
		WHERE t.org_id = $1 ORDER BY t.shop_id, length(t.code), t.code`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (api.Terminal, error) { return scanTerminal(r) })
}

// CreateTerminal adds a till to a shop, named T1, T2… in order, with a
// pairing code. The shop must have an active till manager, so that someone
// can sign in to the till once it has synced.
func (s *Store) CreateTerminal(ctx context.Context, orgID uuid.UUID, in api.TerminalInput, codeHash []byte, now time.Time) (api.TerminalPairing, error) {
	var p api.TerminalPairing
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var shopName string
		err := tx.QueryRow(ctx, "SELECT name FROM shops WHERE id = $1 AND org_id = $2", in.ShopId, orgID).Scan(&shopName)
		if errors.Is(err, pgx.ErrNoRows) {
			return &Problem{ErrNotFound, "That shop doesn't exist."}
		}
		if err != nil {
			return err
		}
		var managers int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM staff s JOIN staff_shops ss ON ss.staff_id = s.id
			WHERE ss.shop_id = $1 AND s.active AND s.role = 'manager'`, in.ShopId).Scan(&managers); err != nil {
			return err
		}
		if managers == 0 {
			return &Problem{ErrConflict, fmt.Sprintf(
				"Add a till manager for %s on the Staff page first, so someone can sign in to the till.", shopName)}
		}
		codes, err := tx.Query(ctx, "SELECT code FROM terminals WHERE shop_id = $1", in.ShopId)
		if err != nil {
			return err
		}
		taken, err := pgx.CollectRows(codes, pgx.RowTo[string])
		if err != nil {
			return err
		}
		next := 1
		for _, c := range taken {
			if n, err := strconv.Atoi(strings.TrimPrefix(c, "T")); err == nil && n >= next {
				next = n + 1
			}
		}
		name := ""
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		p.Terminal = api.Terminal{Id: newID(), ShopId: in.ShopId, Code: fmt.Sprintf("T%d", next), Name: name,
			Status: api.Waiting}
		if _, err := tx.Exec(ctx, "INSERT INTO terminals (id, org_id, shop_id, code, name) VALUES ($1, $2, $3, $4, $5)",
			p.Terminal.Id, orgID, in.ShopId, p.Terminal.Code, name); err != nil {
			return err
		}
		p.ExpiresAt, err = addPairingCode(ctx, tx, p.Terminal.Id, codeHash, now)
		return err
	})
	return p, err
}

func addPairingCode(ctx context.Context, tx pgx.Tx, terminalID uuid.UUID, codeHash []byte, now time.Time) (time.Time, error) {
	// A new code ends the ones before it.
	if _, err := tx.Exec(ctx, "UPDATE pairing_codes SET used_at = $2 WHERE terminal_id = $1 AND used_at IS NULL",
		terminalID, now); err != nil {
		return time.Time{}, err
	}
	expires := now.Add(PairingLifetime)
	_, err := tx.Exec(ctx, "INSERT INTO pairing_codes (code_hash, terminal_id, expires_at) VALUES ($1, $2, $3)",
		codeHash, terminalID, expires)
	return expires, err
}

// NewPairingCode gives a till a new pairing code.
func (s *Store) NewPairingCode(ctx context.Context, orgID, id uuid.UUID, codeHash []byte, now time.Time) (api.TerminalPairing, error) {
	var p api.TerminalPairing
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		p.Terminal, err = scanTerminal(tx.QueryRow(ctx, "SELECT "+terminalCols+" FROM terminals t WHERE t.id = $1 AND t.org_id = $2",
			id, orgID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		p.ExpiresAt, err = addPairingCode(ctx, tx, id, codeHash, now)
		return err
	})
	return p, err
}

// UnpairTerminal ends a till's device token and its pairing codes.
func (s *Store) UnpairTerminal(ctx context.Context, orgID, id uuid.UUID, now time.Time) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE terminals SET token_hash = NULL, revoked_at = $3 WHERE id = $1 AND org_id = $2",
			id, orgID, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, "UPDATE pairing_codes SET used_at = $2 WHERE terminal_id = $1 AND used_at IS NULL", id, now)
		return err
	})
}

// Pair uses a pairing code: the till gets the token whose hash is given,
// replacing any device paired before.
func (s *Store) Pair(ctx context.Context, codeHash, tokenHash []byte, now time.Time) (api.Pairing, error) {
	var p api.Pairing
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE pairing_codes SET used_at = $2
			WHERE code_hash = $1 AND used_at IS NULL AND expires_at > $2 RETURNING terminal_id`, codeHash, now).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE terminals SET token_hash = $2, paired_at = $3, revoked_at = NULL, last_seen_at = $3
			WHERE id = $1`, id, tokenHash, now); err != nil {
			return err
		}
		p.Terminal, err = scanTerminal(tx.QueryRow(ctx, "SELECT "+terminalCols+" FROM terminals t WHERE t.id = $1", id))
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT o.id, o.name, sh.id, sh.name, sh.currency, sh.timezone
			FROM terminals t JOIN organizations o ON o.id = t.org_id JOIN shops sh ON sh.id = t.shop_id WHERE t.id = $1`, id).
			Scan(&p.Organization.Id, &p.Organization.Name, &p.Shop.Id, &p.Shop.Name, &p.Shop.Currency, &p.Shop.Timezone)
	})
	return p, err
}

// TerminalByToken returns the paired till of a device token, and notes
// that it was seen.
func (s *Store) TerminalByToken(ctx context.Context, tokenHash []byte, now time.Time) (Terminal, error) {
	var t Terminal
	err := s.pool.QueryRow(ctx, `UPDATE terminals SET last_seen_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id, org_id, shop_id, code`, tokenHash, now).
		Scan(&t.ID, &t.OrgID, &t.ShopID, &t.Code)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}
