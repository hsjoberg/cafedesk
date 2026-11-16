// Package backup writes the database to a file and reads it back.
//
// A backup is gzipped text: a header, then for each table a COPY block, as
// pg_dump writes in its plain format. It holds data, not schema: a restore
// makes the schema with the server's own migrations, up to the version the
// backup was taken at, loads the data, then migrates the rest of the way.
// So it needs no Postgres tools, on Railway or on a shop's PC. With the
// schema in place (`server migrate`), psql can load one too.
package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"cafedesk/apps/server/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// skipped are tables a backup leaves out: sign-in sessions and pairing
// codes, which last days or minutes, and the backups' own log.
var skipped = map[string]bool{"schema_migrations": true, "sessions": true, "pairing_codes": true, "backups": true}

const magic = "-- Cafedesk backup"

type table struct {
	name string
	cols []string
}

// tables lists the tables a backup holds, each after the tables it refers
// to, so a restore can load them in order.
func tables(ctx context.Context, tx pgx.Tx) ([]table, error) {
	rows, err := tx.Query(ctx, `SELECT c.table_name, array_agg(c.column_name::text ORDER BY c.ordinal_position)
		FROM information_schema.columns c JOIN information_schema.tables t
			ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'
		GROUP BY c.table_name ORDER BY c.table_name`)
	if err != nil {
		return nil, err
	}
	all := map[string][]string{}
	var names []string
	for rows.Next() {
		var name string
		var cols []string
		if err := rows.Scan(&name, &cols); err != nil {
			return nil, err
		}
		if !skipped[name] {
			all[name] = cols
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT cl.relname::text, ref.relname::text FROM pg_constraint co
		JOIN pg_class cl ON cl.oid = co.conrelid JOIN pg_class ref ON ref.oid = co.confrelid
		JOIN pg_namespace n ON n.oid = cl.relnamespace
		WHERE co.contype = 'f' AND n.nspname = 'public' AND cl.relname <> ref.relname`)
	if err != nil {
		return nil, err
	}
	deps := map[string][]string{}
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		deps[from] = append(deps[from], to)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Depth-first, in name order, so the order is the same every time.
	var out []table
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(string) error
	visit = func(n string) error {
		switch state[n] {
		case 1:
			return fmt.Errorf("tables refer to each other in a loop, at %s", n)
		case 2:
			return nil
		}
		state[n] = 1
		d := deps[n]
		sort.Strings(d)
		for _, m := range d {
			if _, ok := all[m]; ok {
				if err := visit(m); err != nil {
					return err
				}
			}
		}
		state[n] = 2
		out = append(out, table{n, all[n]})
		return nil
	}
	for _, n := range names {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func quoteCols(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = pgx.Identifier{c}.Sanitize()
	}
	return strings.Join(q, ", ")
}

// Info is what a backup's header says.
type Info struct {
	Schema int
	Taken  time.Time
}

// Write writes the whole database, as one consistent snapshot, gzipped.
func Write(ctx context.Context, pool *pgxpool.Pool, w io.Writer, now time.Time) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Dates as ISO, whatever the server's settings, as the header tells psql.
	if _, err := tx.Exec(ctx, "SET LOCAL datestyle = 'ISO, YMD'"); err != nil {
		return err
	}
	var schema int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&schema); err != nil {
		return err
	}
	list, err := tables(ctx, tx)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(w)
	bw := bufio.NewWriter(gz)
	fmt.Fprintf(bw, "%s\n-- schema: %d\n-- taken: %s\n--\n", magic, schema, now.UTC().Format(time.RFC3339))
	fmt.Fprint(bw, "-- To load with psql: make the schema first (server migrate), then psql -1 -f this file.\n\n")
	fmt.Fprint(bw, "SET client_encoding = 'UTF8';\nSET datestyle = 'ISO, YMD';\n\n")
	for _, t := range list {
		cols := quoteCols(t.cols)
		fmt.Fprintf(bw, "COPY %s (%s) FROM stdin;\n", pgx.Identifier{t.name}.Sanitize(), cols)
		if err := bw.Flush(); err != nil {
			return err
		}
		if _, err := conn.Conn().PgConn().CopyTo(ctx, bw, fmt.Sprintf("COPY %s (%s) TO STDOUT",
			pgx.Identifier{t.name}.Sanitize(), cols)); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		fmt.Fprint(bw, "\\.\n\n")
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReadInfo reads a backup's header.
func ReadInfo(br *bufio.Reader) (Info, error) {
	var info Info
	first, err := br.ReadString('\n')
	if err != nil || strings.TrimSpace(first) != magic {
		return info, errors.New("this isn't a Cafedesk backup")
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return info, errors.New("the backup's header is cut short")
		}
		line = strings.TrimSpace(line)
		if line == "--" {
			break
		}
		if v, ok := strings.CutPrefix(line, "-- schema: "); ok {
			info.Schema, _ = strconv.Atoi(v)
		}
		if v, ok := strings.CutPrefix(line, "-- taken: "); ok {
			info.Taken, _ = time.Parse(time.RFC3339, v)
		}
	}
	if info.Schema == 0 {
		return info, errors.New("the backup doesn't say its schema version")
	}
	return info, nil
}

// Restore loads a backup into a new, empty database: it makes the schema
// the backup was taken with, loads every table in one transaction, then
// migrates to this server's schema.
func Restore(ctx context.Context, pool *pgxpool.Pool, r io.Reader) (Info, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Info{}, errors.New("this isn't a Cafedesk backup (not gzipped)")
	}
	br := bufio.NewReaderSize(gz, 1<<16)
	info, err := ReadInfo(br)
	if err != nil {
		return info, err
	}
	if info.Schema > db.Latest() {
		return info, fmt.Errorf("the backup is from a newer server (schema %d; this one knows %d): restore it with that version", info.Schema, db.Latest())
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return info, err
	}
	if exists {
		var current, orgs int
		if err := pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
			return info, err
		}
		if current > 0 {
			if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM organizations").Scan(&orgs); err != nil {
				return info, err
			}
		}
		if orgs > 0 {
			return info, errors.New("this database has data already. Restore into a new, empty one")
		}
		if current > info.Schema {
			return info, fmt.Errorf("this database's schema (%d) is newer than the backup's (%d). Restore into a new, empty database, before the server starts on it", current, info.Schema)
		}
	}
	if _, err := db.MigrateTo(ctx, pool, info.Schema); err != nil {
		return info, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return info, err
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return info, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL datestyle = 'ISO, YMD'"); err != nil {
		return info, err
	}
	for {
		line, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return info, err
		}
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, "--") || strings.HasPrefix(line, "SET ") {
			continue
		}
		head, ok := strings.CutSuffix(line, " FROM stdin;")
		if !ok || !strings.HasPrefix(head, "COPY ") {
			return info, fmt.Errorf("the backup has a line it shouldn't: %.60s", line)
		}
		block := &copyBlock{r: br}
		if _, err := conn.Conn().PgConn().CopyFrom(ctx, block, head+" FROM STDIN"); err != nil {
			return info, fmt.Errorf("%s: %w", strings.TrimPrefix(head, "COPY "), err)
		}
		if !block.ended {
			return info, errors.New("the backup is cut short")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return info, err
	}
	_, err = db.Migrate(ctx, pool)
	return info, err
}

// copyBlock reads one COPY block's rows, up to its \. line.
type copyBlock struct {
	r     *bufio.Reader
	buf   []byte
	ended bool
}

func (b *copyBlock) Read(p []byte) (int, error) {
	for len(b.buf) == 0 {
		if b.ended {
			return 0, io.EOF
		}
		line, err := b.r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return 0, io.EOF // cut short: ended stays false
		}
		if string(line) == "\\.\n" || string(line) == "\\." {
			b.ended = true
			return 0, io.EOF
		}
		b.buf = line
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	return n, nil
}
