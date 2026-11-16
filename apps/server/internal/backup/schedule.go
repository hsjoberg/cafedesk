package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Hour is when the nightly backup runs, in the first shop's time: after a
// late close, before the morning.
const Hour = 3

// lock is the advisory lock a backup holds, so two servers on the same
// database don't both take one.
const lock = 7_204_118_302

// Run takes one backup now and keeps it in every destination, pruning the
// old ones there. It logs the attempt in the backups table. The backup is
// named for the date in the first shop's time.
func Run(ctx context.Context, pool *pgxpool.Pool, dests []Destination, now time.Time) (name string, size int, err error) {
	if len(dests) == 0 {
		return "", 0, errors.New("no backup destination is set")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return "", 0, err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lock).Scan(&got); err != nil {
		return "", 0, err
	}
	if !got {
		return "", 0, errors.New("another backup is running")
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lock)

	id, loc := uuid.New(), shopLocation(ctx, pool)
	name = "cafedesk-" + now.In(loc).Format("2006-01-02") + ".sql.gz"
	var where []string
	for _, d := range dests {
		where = append(where, d.String())
	}
	if _, err := pool.Exec(ctx, "INSERT INTO backups (id, started_at, name, destinations) VALUES ($1, $2, $3, $4)",
		id, now, name, strings.Join(where, ", ")); err != nil {
		return name, 0, err
	}
	defer func() {
		var msg *string
		if err != nil {
			s := err.Error()
			msg = &s
		}
		pool.Exec(context.Background(), "UPDATE backups SET finished_at = now(), bytes = $2, error = $3 WHERE id = $1", id, size, msg)
	}()

	var buf bytes.Buffer
	if err := Write(ctx, pool, &buf, now); err != nil {
		return name, 0, err
	}
	size = buf.Len()
	var failed []string
	for _, d := range dests {
		if err := d.Put(ctx, name, buf.Bytes()); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", d, err))
			continue
		}
		if err := Prune(ctx, d); err != nil {
			failed = append(failed, fmt.Sprintf("%s, removing old backups: %v", d, err))
		}
	}
	if len(failed) > 0 {
		return name, size, errors.New(strings.Join(failed, "; "))
	}
	return name, size, nil
}

// shopLocation is the first shop's time zone, or Dubai's before there is a
// shop.
func shopLocation(ctx context.Context, pool *pgxpool.Pool) *time.Location {
	tz := "Asia/Dubai"
	err := pool.QueryRow(ctx, "SELECT timezone FROM shops ORDER BY created_at LIMIT 1").Scan(&tz)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		tz = "Asia/Dubai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Due reports whether the nightly backup should run now: it is past Hour in
// the shop's time, none has succeeded since then, and none has been tried in
// the last hour. A server restarted in the morning catches up.
func Due(ctx context.Context, pool *pgxpool.Pool, now time.Time) (bool, error) {
	local := now.In(shopLocation(ctx, pool))
	since := time.Date(local.Year(), local.Month(), local.Day(), Hour, 0, 0, 0, local.Location())
	if local.Before(since) {
		since = since.AddDate(0, 0, -1)
	}
	var ok, tried bool
	err := pool.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM backups WHERE started_at >= $1 AND finished_at IS NOT NULL AND error IS NULL),
			EXISTS (SELECT 1 FROM backups WHERE started_at > $2)`, since, now.Add(-time.Hour)).Scan(&ok, &tried)
	return !ok && !tried, err
}

// Nightly takes the nightly backup whenever it is due, until ctx ends.
func Nightly(ctx context.Context, pool *pgxpool.Pool, dests []Destination, log *slog.Logger) {
	check := func() {
		due, err := Due(ctx, pool, time.Now())
		if err != nil || !due {
			if err != nil && ctx.Err() == nil {
				log.Error("backup: checking", "err", err)
			}
			return
		}
		start := time.Now()
		name, size, err := Run(ctx, pool, dests, start)
		if err != nil {
			log.Error("backup failed", "name", name, "err", err)
			return
		}
		log.Info("backup", "name", name, "bytes", size, "took", time.Since(start).Round(time.Millisecond).String())
	}
	check()
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}
