package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupsKept is how many backups a folder keeps; older ones are deleted.
const backupsKept = 30

// backupEvery is how often the till backs up while it runs, besides after
// closing the day.
const backupEvery = 4 * time.Hour

// defaultBackupDir is used until a folder is chosen in Settings: a second
// copy on the same disk, which survives a damaged database but not a dead
// disk. A USB drive is better.
func defaultBackupDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents", "Cafedesk backups")
}

// Backup copies the database into dir as cafedesk-YYYYMMDD-HHMMSS.db, and
// deletes the oldest backups beyond backupsKept. Sales wait while it runs,
// which takes a moment.
func (s *Store) Backup(ctx context.Context, dir string, now time.Time) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("no backup folder is set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "cafedesk-"+now.Format("20060102-150405")+".db")
	if err := s.copyTo(ctx, dest); err != nil {
		return "", err
	}
	_ = s.SetMeta(ctx, "last_backup", ts(now))
	_ = s.SetMeta(ctx, "last_backup_path", dest)
	pruneBackups(dir)
	return dest, nil
}

func (s *Store) copyTo(ctx context.Context, dest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Move everything from the write-ahead log into the database file, so
	// the file alone is the whole database.
	if _, err := s.db.Query(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	in, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func pruneBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); strings.HasPrefix(n, "cafedesk-") && strings.HasSuffix(n, ".db") {
			names = append(names, n)
		}
	}
	sort.Strings(names) // the time in the name sorts them oldest first
	for len(names) > backupsKept {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// LastBackup returns when and where the latest backup was made.
func (s *Store) LastBackup(ctx context.Context) (time.Time, string) {
	at, _ := s.Meta(ctx, "last_backup")
	path, _ := s.Meta(ctx, "last_backup_path")
	if at == "" {
		return time.Time{}, ""
	}
	return parseTS(at), path
}
