package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/google/uuid"
)

// Store is the terminal's local database: the source of truth for sales
// until they are synced. Every sale, void, refund and closed day also gets
// an outbox row, which the sync worker (Phase 2) sends and marks as sent.
type Store struct {
	db   *sqlite.DB
	path string
	// mu lets any number of queries run at once, and a backup alone.
	mu sync.RWMutex
}

// schema is the database's migrations, in order. PRAGMA user_version is
// how many have run. Add new ones at the end; never edit one that shipped.
var schema = [][]string{
	// 1: Phase 1.
	{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE categories (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, name_ar TEXT NOT NULL DEFAULT '',
			color TEXT NOT NULL, position INTEGER NOT NULL, kitchen INTEGER NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE items (
			id TEXT PRIMARY KEY, category_id TEXT NOT NULL REFERENCES categories(id),
			name TEXT NOT NULL, name_ar TEXT NOT NULL DEFAULT '', price_fils INTEGER NOT NULL,
			position INTEGER NOT NULL, kitchen INTEGER NOT NULL DEFAULT 0, active INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE modifier_groups (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, name_ar TEXT NOT NULL DEFAULT '',
			multiple INTEGER NOT NULL DEFAULT 0, position INTEGER NOT NULL)`,
		`CREATE TABLE modifiers (
			id TEXT PRIMARY KEY, group_id TEXT NOT NULL REFERENCES modifier_groups(id),
			name TEXT NOT NULL, name_ar TEXT NOT NULL DEFAULT '', price_delta_fils INTEGER NOT NULL DEFAULT 0,
			is_default INTEGER NOT NULL DEFAULT 0, position INTEGER NOT NULL)`,
		`CREATE TABLE item_modifier_groups (
			item_id TEXT NOT NULL REFERENCES items(id), group_id TEXT NOT NULL REFERENCES modifier_groups(id),
			position INTEGER NOT NULL, PRIMARY KEY (item_id, group_id))`,
		`CREATE TABLE staff (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, role TEXT NOT NULL, pin_hash TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1, must_change_pin INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE business_days (
			id TEXT PRIMARY KEY, org_id TEXT NOT NULL, shop_id TEXT NOT NULL, terminal_id TEXT NOT NULL,
			date TEXT NOT NULL, opened_at TEXT NOT NULL, opened_by TEXT NOT NULL REFERENCES staff(id),
			opening_float_fils INTEGER NOT NULL, closed_at TEXT, closed_by TEXT REFERENCES staff(id),
			counted_cash_fils INTEGER, report TEXT)`,
		`CREATE TABLE orders (
			id TEXT PRIMARY KEY, org_id TEXT NOT NULL, shop_id TEXT NOT NULL, terminal_id TEXT NOT NULL,
			business_day_id TEXT NOT NULL REFERENCES business_days(id), number INTEGER NOT NULL,
			code TEXT NOT NULL, created_at TEXT NOT NULL, staff_id TEXT NOT NULL REFERENCES staff(id),
			subtotal_fils INTEGER NOT NULL, tax_fils INTEGER NOT NULL DEFAULT 0, total_fils INTEGER NOT NULL,
			payment_method TEXT NOT NULL, tendered_fils INTEGER NOT NULL DEFAULT 0,
			change_fils INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'completed',
			voided_at TEXT, voided_by TEXT REFERENCES staff(id), void_reason TEXT,
			UNIQUE (business_day_id, number))`,
		`CREATE TABLE order_lines (
			id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES orders(id),
			org_id TEXT NOT NULL, shop_id TEXT NOT NULL, position INTEGER NOT NULL,
			item_id TEXT NOT NULL, name TEXT NOT NULL, qty INTEGER NOT NULL,
			unit_price_fils INTEGER NOT NULL, unit_cost_fils INTEGER,
			tax_rate_bp INTEGER NOT NULL DEFAULT 0, tax_fils INTEGER NOT NULL DEFAULT 0,
			line_total_fils INTEGER NOT NULL, kitchen INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE order_line_modifiers (
			id TEXT PRIMARY KEY, line_id TEXT NOT NULL REFERENCES order_lines(id),
			order_id TEXT NOT NULL REFERENCES orders(id), position INTEGER NOT NULL,
			group_id TEXT NOT NULL, group_name TEXT NOT NULL, modifier_id TEXT NOT NULL,
			name TEXT NOT NULL, price_delta_fils INTEGER NOT NULL, is_default INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE refunds (
			id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES orders(id),
			org_id TEXT NOT NULL, shop_id TEXT NOT NULL, terminal_id TEXT NOT NULL,
			business_day_id TEXT NOT NULL REFERENCES business_days(id), created_at TEXT NOT NULL,
			staff_id TEXT NOT NULL REFERENCES staff(id), approved_by TEXT NOT NULL REFERENCES staff(id),
			reason TEXT NOT NULL, method TEXT NOT NULL, total_fils INTEGER NOT NULL)`,
		`CREATE TABLE refund_lines (
			id TEXT PRIMARY KEY, refund_id TEXT NOT NULL REFERENCES refunds(id),
			order_line_id TEXT NOT NULL REFERENCES order_lines(id), qty INTEGER NOT NULL,
			amount_fils INTEGER NOT NULL)`,
		`CREATE TABLE outbox (
			seq INTEGER PRIMARY KEY AUTOINCREMENT, entity TEXT NOT NULL, entity_id TEXT NOT NULL,
			created_at TEXT NOT NULL, sent_at TEXT)`,
		`CREATE INDEX outbox_unsent ON outbox(sent_at) WHERE sent_at IS NULL`,
		`CREATE INDEX orders_day ON orders(business_day_id)`,
		`CREATE INDEX order_lines_order ON order_lines(order_id)`,
		`CREATE INDEX refunds_order ON refunds(order_id)`,
		`CREATE TABLE checks (id INTEGER PRIMARY KEY AUTOINCREMENT, note TEXT NOT NULL, created_at TEXT NOT NULL)`,
	},
	// 2: Phase 2, syncing with the server.
	{
		`ALTER TABLE modifier_groups ADD COLUMN active INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE modifiers ADD COLUMN active INTEGER NOT NULL DEFAULT 1`,
		// 'local' for staff made on this till, 'server' for staff from the admin.
		`ALTER TABLE staff ADD COLUMN source TEXT NOT NULL DEFAULT 'local'`,
		// What an event must carry as it was, such as the PIN a change replaced.
		`ALTER TABLE outbox ADD COLUMN payload TEXT`,
	},
	// 3: the drawer carries over from day to day, and cash put in or taken
	// out. A day closed with no closed_by closed by itself; one with no
	// counted_cash_fils wasn't counted.
	{
		`CREATE TABLE cash_movements (
			id TEXT PRIMARY KEY, org_id TEXT NOT NULL, shop_id TEXT NOT NULL, terminal_id TEXT NOT NULL,
			business_day_id TEXT NOT NULL REFERENCES business_days(id), created_at TEXT NOT NULL,
			staff_id TEXT NOT NULL REFERENCES staff(id), approved_by TEXT NOT NULL REFERENCES staff(id),
			amount_fils INTEGER NOT NULL, reason TEXT NOT NULL)`,
		`CREATE INDEX cash_movements_day ON cash_movements(business_day_id)`,
	},
}

// OpenStore opens or creates pos.db in dir.
func OpenStore(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "pos.db")
	if err := setAsidePhase0(ctx, path); err != nil {
		return nil, err
	}
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// setAsidePhase0 renames a Phase 0 database, which held test sales in an
// older layout, so the terminal starts afresh.
func setAsidePhase0(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	version, _ := db.Query(ctx, "PRAGMA user_version")
	tables, _ := db.Query(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'orders'")
	db.Close()
	if firstInt(version) != 0 || firstInt(tables) == 0 {
		return nil
	}
	aside := strings.TrimSuffix(path, ".db") + "-phase0-" + time.Now().Format("20060102-150405") + ".db"
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			if err := os.Rename(path+suffix, aside+suffix); err != nil {
				return fmt.Errorf("set aside the Phase 0 database: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	// WAL keeps the database whole through a power cut mid-write.
	if _, err := s.db.Query(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("journal mode: %w", err)
	}
	if _, err := s.db.Execute(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("foreign keys: %w", err)
	}
	rows, err := s.db.Query(ctx, "PRAGMA user_version")
	if err != nil {
		return err
	}
	for v := int(firstInt(rows)); v < len(schema); v++ {
		stmts := make([]sqlite.Statement, 0, len(schema[v])+1)
		for _, sql := range schema[v] {
			stmts = append(stmts, sqlite.Statement{SQL: sql})
		}
		stmts = append(stmts, sqlite.Statement{SQL: fmt.Sprintf("PRAGMA user_version = %d", v+1)})
		if _, err := s.db.Transaction(ctx, stmts); err != nil {
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// HasHistory reports whether the till holds any days or sales.
func (s *Store) HasHistory(ctx context.Context) (bool, error) {
	rows, err := s.query(ctx, "SELECT EXISTS (SELECT 1 FROM business_days) OR EXISTS (SELECT 1 FROM orders)")
	return firstInt(rows) == 1, err
}

// StartFresh sets the database aside as pos-<label>-<date>.db, beside it,
// and starts an empty one in its place: a till used for trying out keeps
// its test sales there, out of the admin's reports. It returns where they
// went.
func (s *Store) StartFresh(ctx context.Context, label string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.Close(); err != nil {
		return "", err
	}
	aside := strings.TrimSuffix(s.path, ".db") + "-" + label + "-" + time.Now().Format("20060102-150405") + ".db"
	var moveErr error
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(s.path + suffix); err == nil && moveErr == nil {
			moveErr = os.Rename(s.path+suffix, aside+suffix)
		}
	}
	db, err := sqlite.Open(ctx, s.path, sqlite.OpenOptions{})
	if err != nil {
		return "", fmt.Errorf("open %s: %w", s.path, err)
	}
	s.db = db
	if moveErr != nil {
		return "", fmt.Errorf("set the old sales aside: %w", moveErr)
	}
	return aside, s.migrate(ctx)
}

// Path is the database file.
func (s *Store) Path() string { return s.path }

func (s *Store) exec(ctx context.Context, sql string, args ...any) (sqlite.Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Execute(ctx, sql, args...)
}

func (s *Store) query(ctx context.Context, sql string, args ...any) (sqlite.Rows, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Query(ctx, sql, args...)
}

func (s *Store) tx(ctx context.Context, stmts []sqlite.Statement) ([]sqlite.Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Transaction(ctx, stmts)
}

func stmt(sql string, args ...any) sqlite.Statement { return sqlite.Statement{SQL: sql, Args: args} }

func outboxStmt(entity, id string, at time.Time) sqlite.Statement {
	return stmt("INSERT INTO outbox (entity, entity_id, created_at) VALUES (?, ?, ?)", entity, id, ts(at))
}

// outboxIfChanged adds an outbox row only if the statement before it in the
// transaction changed a row: a void or a close that didn't happen syncs
// nothing.
func outboxIfChanged(entity, id string, at time.Time) sqlite.Statement {
	return stmt("INSERT INTO outbox (entity, entity_id, created_at) SELECT ?, ?, ? WHERE changes() = 1",
		entity, id, ts(at))
}

// The catalog.

// LoadCatalog reads the active menu. In demo mode, a new database gets the
// demo menu; otherwise the menu comes from the admin.
func (s *Store) LoadCatalog(ctx context.Context, demo bool) (*Catalog, error) {
	rows, err := s.query(ctx, "SELECT COUNT(*) FROM categories")
	if err != nil {
		return nil, err
	}
	if demo && firstInt(rows) == 0 {
		if err := s.SaveCatalog(ctx, demoCatalog()); err != nil {
			return nil, fmt.Errorf("seed the menu: %w", err)
		}
	}
	c := &Catalog{Groups: map[string]ModifierGroup{}}
	rows, err = s.query(ctx, `SELECT id, name, name_ar, color, position, kitchen FROM categories
		WHERE active = 1 ORDER BY position`)
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		c.Categories = append(c.Categories, Category{ID: str(r[0]), Name: str(r[1]), NameAR: str(r[2]),
			Color: str(r[3]), Position: int(i64(r[4])), Kitchen: i64(r[5]) != 0})
	}
	rows, err = s.query(ctx, "SELECT id, name, name_ar, multiple, position FROM modifier_groups WHERE active = 1 ORDER BY position")
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		c.Groups[str(r[0])] = ModifierGroup{ID: str(r[0]), Name: str(r[1]), NameAR: str(r[2]),
			Multiple: i64(r[3]) != 0, Position: int(i64(r[4]))}
	}
	rows, err = s.query(ctx, `SELECT id, group_id, name, name_ar, price_delta_fils, is_default, position
		FROM modifiers WHERE active = 1 ORDER BY group_id, position`)
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		g, ok := c.Groups[str(r[1])]
		if !ok {
			continue // of a deleted group
		}
		g.Modifiers = append(g.Modifiers, Modifier{ID: str(r[0]), GroupID: str(r[1]), Name: str(r[2]),
			NameAR: str(r[3]), Delta: money.Fils(i64(r[4])), Default: i64(r[5]) != 0, Position: int(i64(r[6]))})
		c.Groups[str(r[1])] = g
	}
	links, err := s.query(ctx, "SELECT item_id, group_id FROM item_modifier_groups ORDER BY item_id, position")
	if err != nil {
		return nil, err
	}
	groups := map[string][]string{}
	for _, r := range links.Values {
		if _, ok := c.Groups[str(r[1])]; ok {
			groups[str(r[0])] = append(groups[str(r[0])], str(r[1]))
		}
	}
	rows, err = s.query(ctx, `SELECT i.id, i.category_id, i.name, i.name_ar, i.price_fils, i.position, i.kitchen
		FROM items i JOIN categories c ON c.id = i.category_id
		WHERE i.active = 1 AND c.active = 1 ORDER BY c.position, i.position`)
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		id := str(r[0])
		c.Items = append(c.Items, Item{ID: id, CategoryID: str(r[1]), Name: str(r[2]), NameAR: str(r[3]),
			Price: money.Fils(i64(r[4])), Position: int(i64(r[5])), Kitchen: i64(r[6]) != 0, Groups: groups[id]})
	}
	return c, nil
}

// SaveCatalog writes a whole menu, adding or replacing its rows. Phase 2's
// sync uses it for what the server sends.
func (s *Store) SaveCatalog(ctx context.Context, c *Catalog) error {
	var stmts []sqlite.Statement
	for _, cat := range c.Categories {
		stmts = append(stmts, stmt(`INSERT OR REPLACE INTO categories (id, name, name_ar, color, position, kitchen, active)
			VALUES (?, ?, ?, ?, ?, ?, 1)`, cat.ID, cat.Name, cat.NameAR, cat.Color, cat.Position, b2i(cat.Kitchen)))
	}
	for _, g := range c.Groups {
		stmts = append(stmts, stmt(`INSERT OR REPLACE INTO modifier_groups (id, name, name_ar, multiple, position)
			VALUES (?, ?, ?, ?, ?)`, g.ID, g.Name, g.NameAR, b2i(g.Multiple), g.Position))
		for _, m := range g.Modifiers {
			stmts = append(stmts, stmt(`INSERT OR REPLACE INTO modifiers (id, group_id, name, name_ar,
				price_delta_fils, is_default, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				m.ID, g.ID, m.Name, m.NameAR, int64(m.Delta), b2i(m.Default), m.Position))
		}
	}
	for _, it := range c.Items {
		stmts = append(stmts, stmt(`INSERT OR REPLACE INTO items (id, category_id, name, name_ar, price_fils,
			position, kitchen, active) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
			it.ID, it.CategoryID, it.Name, it.NameAR, int64(it.Price), it.Position, b2i(it.Kitchen)))
		stmts = append(stmts, stmt("DELETE FROM item_modifier_groups WHERE item_id = ?", it.ID))
		for i, g := range it.Groups {
			stmts = append(stmts, stmt(`INSERT INTO item_modifier_groups (item_id, group_id, position)
				VALUES (?, ?, ?)`, it.ID, g, i))
		}
	}
	_, err := s.tx(ctx, stmts)
	return err
}

// Meta values: small settings kept with the data, such as the last backup.

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	rows, err := s.query(ctx, "SELECT value FROM meta WHERE key = ?", key)
	if err != nil || len(rows.Values) == 0 {
		return "", err
	}
	return str(rows.Values[0][0]), nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.exec(ctx, "INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, value)
	return err
}

// The Checks screen.

// StoreStats is what the Checks screen shows about the database.
type StoreStats struct {
	Orders int
	Unsent int
}

// Stats counts the orders and the outbox rows not yet sent.
func (s *Store) Stats(ctx context.Context) (StoreStats, error) {
	var st StoreStats
	rows, err := s.query(ctx, "SELECT COUNT(*) FROM orders")
	if err != nil {
		return st, err
	}
	st.Orders = int(firstInt(rows))
	rows, err = s.query(ctx, "SELECT COUNT(*) FROM outbox WHERE sent_at IS NULL")
	if err != nil {
		return st, err
	}
	st.Unsent = int(firstInt(rows))
	return st, nil
}

// WriteCheck writes a row and reads it back. It returns how many check rows
// the database holds, which grows across restarts if the file survives them.
func (s *Store) WriteCheck(ctx context.Context) (int, error) {
	note := "check " + time.Now().Format(time.RFC3339Nano)
	res, err := s.exec(ctx, "INSERT INTO checks (note, created_at) VALUES (?, ?)", note, ts(time.Now()))
	if err != nil {
		return 0, err
	}
	rows, err := s.query(ctx, "SELECT note FROM checks WHERE id = ?", res.LastInsertID)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) != 1 || rows.Values[0][0] != note {
		return 0, fmt.Errorf("read back %v, want %q", rows.Values, note)
	}
	rows, err = s.query(ctx, "SELECT COUNT(*) FROM checks")
	if err != nil {
		return 0, err
	}
	return int(firstInt(rows)), nil
}

// Values from rows.

func firstInt(rows sqlite.Rows) int64 {
	if len(rows.Values) == 0 || len(rows.Values[0]) == 0 {
		return 0
	}
	return i64(rows.Values[0][0])
}

func i64(v any) int64 {
	n, _ := v.(int64)
	return n
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// ts formats a time for the database: UTC, to the nanosecond.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// parseTS reads a time from the database, in local time.
func parseTS(v any) time.Time {
	t, err := time.Parse(time.RFC3339Nano, str(v))
	if err != nil {
		return time.Time{}
	}
	return t.Local()
}

// newID returns a UUIDv7: unique without asking the server, and in time
// order, which keeps indexes compact.
func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

var errNotFound = errors.New("not found")
