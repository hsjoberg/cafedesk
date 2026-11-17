package backup_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/backup"
	"cafedesk/apps/server/internal/db"
	"cafedesk/apps/server/internal/store"
	"cafedesk/apps/server/internal/testdb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// filled returns a database with a demo shop and a few days of sales, and
// someone whose name needs escaping in a COPY block.
func filled(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	st := store.New(pool)
	org, shop, user, err := st.Setup(ctx, "Karak Corner", "Downtown", "Owner", "owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDemoMenu(ctx, org.ID); err != nil {
		t.Fatal(err)
	}
	var people []store.DemoStaff
	for _, name := range []string{"Ravi", "Anil \\ O'Brien\twith a tab\nand a new line, كرك"} {
		s, err := st.CreateStaff(ctx, org.ID, api.StaffInput{Name: name, Role: api.StaffRoleManager,
			ShopIds: []uuid.UUID{shop.ID}, Active: true}, "hash")
		if err != nil {
			t.Fatal(err)
		}
		people = append(people, store.DemoStaff{ID: s.Id, Name: s.Name})
	}
	if err := st.SeedDemoSales(ctx, org.ID, shop.ID, people[0], people, 5, time.Now(), 2); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, user.ID, []byte("token-hash"), time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatal(err)
	}
	return pool
}

// fingerprints sums every table's rows, to compare two databases.
func fingerprints(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT table_name::text FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	out := map[string]string{}
	for _, n := range names {
		var sum string
		if err := pool.QueryRow(ctx, "SELECT COUNT(*)::text || ' ' || COALESCE(md5(string_agg(x::text, '|' ORDER BY x::text)), '') FROM "+
			n+" x").Scan(&sum); err != nil {
			t.Fatal(err)
		}
		out[n] = sum
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := filled(t)
	var buf bytes.Buffer
	taken := time.Date(2026, 11, 16, 23, 0, 0, 0, time.UTC)
	if err := backup.Write(ctx, src, &buf, taken); err != nil {
		t.Fatal(err)
	}

	dst := testdb.Empty(t)
	info, err := backup.Restore(ctx, dst, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if info.Schema != db.Latest() || !info.Taken.Equal(taken) {
		t.Errorf("info %+v", info)
	}
	want, got := fingerprints(t, src), fingerprints(t, dst)
	for table, sum := range want {
		switch table {
		case "sessions", "pairing_codes", "backups", "schema_migrations":
			continue
		}
		if got[table] != sum {
			t.Errorf("%s: %s, want %s", table, got[table], sum)
		}
	}
	if strings.HasPrefix(want["orders"], "0 ") {
		t.Fatal("no orders to back up")
	}
	if !strings.HasPrefix(got["sessions"], "0 ") {
		t.Errorf("sessions came back: %s", got["sessions"])
	}

	// Not over data, and not anything else.
	if _, err := backup.Restore(ctx, dst, bytes.NewReader(buf.Bytes())); err == nil || !strings.Contains(err.Error(), "data already") {
		t.Errorf("restoring over data: %v", err)
	}
	if _, err := backup.Restore(ctx, testdb.Empty(t), strings.NewReader("not a backup")); err == nil {
		t.Error("restored text that isn't a backup")
	}
	// A backup cut short loads nothing.
	cut := testdb.Empty(t)
	if _, err := backup.Restore(ctx, cut, bytes.NewReader(buf.Bytes()[:buf.Len()*2/3])); err == nil {
		t.Error("restored a backup cut short")
	}
	var orgs int
	cut.QueryRow(ctx, "SELECT COUNT(*) FROM organizations").Scan(&orgs)
	if orgs != 0 {
		t.Errorf("a backup cut short left %d organizations", orgs)
	}
	// A database the server has migrated, still empty, is fine.
	if _, err := backup.Restore(ctx, testdb.New(t), bytes.NewReader(buf.Bytes())); err != nil {
		t.Errorf("into a migrated, empty database: %v", err)
	}
}

func TestPrune(t *testing.T) {
	ctx := context.Background()
	dir := backup.Dir(t.TempDir())
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 70; i++ {
		dir.Put(ctx, "cafedesk-"+day.AddDate(0, 0, i).Format("2006-01-02")+".sql.gz", []byte("x"))
	}
	os.WriteFile(filepath.Join(string(dir), "notes.txt"), []byte("mine"), 0o600)
	if err := backup.Prune(ctx, dir); err != nil {
		t.Fatal(err)
	}
	kept, _ := dir.List(ctx)
	sort.Strings(kept)
	// The latest 30, from 11 Oct to 9 Nov, and the 1st of September and October.
	if len(kept) != 32 || kept[0] != "cafedesk-2026-09-01.sql.gz" || kept[1] != "cafedesk-2026-10-01.sql.gz" ||
		kept[2] != "cafedesk-2026-10-11.sql.gz" || kept[31] != "cafedesk-2026-11-09.sql.gz" {
		t.Errorf("kept %d: %v", len(kept), kept)
	}
	if _, err := os.Stat(filepath.Join(string(dir), "notes.txt")); err != nil {
		t.Error("pruning took a file that isn't a backup")
	}
}

func TestNightly(t *testing.T) {
	ctx := context.Background()
	pool := filled(t)
	dir := backup.Dir(t.TempDir())
	dubai, _ := time.LoadLocation("Asia/Dubai")
	at := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, dubai) }

	if due, _ := backup.Due(ctx, pool, at(16, 2)); !due {
		t.Error("never backed up: not due")
	}
	name, size, err := backup.Run(ctx, pool, []backup.Destination{dir}, at(16, 3))
	if err != nil || name != "cafedesk-2026-11-16.sql.gz" || size == 0 {
		t.Fatalf("run: %s %d %v", name, size, err)
	}
	if b, _ := os.ReadFile(filepath.Join(string(dir), name)); len(b) != size {
		t.Errorf("the file has %d bytes, the backup %d", len(b), size)
	}
	for _, c := range []struct {
		when time.Time
		due  bool
	}{
		{at(16, 4), false}, // done today
		{at(17, 2), false}, // before tonight's
		{at(17, 3), true},
		{at(18, 9), true}, // a missed night is caught up
	} {
		if due, err := backup.Due(ctx, pool, c.when); err != nil || due != c.due {
			t.Errorf("at %s: due %v, want %v (%v)", c.when.Format("2 Jan 15:04"), due, c.due, err)
		}
	}
	// A failed one is retried after an hour, not every ten minutes.
	blocker := filepath.Join(t.TempDir(), "a file")
	os.WriteFile(blocker, nil, 0o600)
	if _, _, err := backup.Run(ctx, pool, []backup.Destination{backup.Dir(filepath.Join(blocker, "x"))}, at(17, 3)); err == nil {
		t.Fatal("a backup to a folder that can't exist worked")
	}
	if due, _ := backup.Due(ctx, pool, at(17, 3).Add(30*time.Minute)); due {
		t.Error("retried within the hour")
	}
	if due, _ := backup.Due(ctx, pool, at(17, 4).Add(5*time.Minute)); !due {
		t.Error("not retried after an hour")
	}
}
