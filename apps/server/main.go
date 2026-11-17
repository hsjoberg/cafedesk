// Command server is the Cafedesk server: the API of api/openapi.yaml and
// the admin, on PostgreSQL.
//
//	server                 serve (migrating the database first)
//	server migrate         migrate the database and exit
//	server setup -org ...  create the organization, its first shop and owner
//	server demo            fill a new, empty database with a demo shop and a
//	                       month of made-up sales, to show or try the admin
//	server backup          write a backup to a file, or with -now to where
//	                       the nightly backups go
//	server restore FILE    load a backup into a new, empty database
//
// Environment: DATABASE_URL (required), PORT (8080), CAFEDESK_TRUST_PROXY=1
// behind a proxy such as Railway's, CAFEDESK_INSECURE_COOKIES=1 only for
// development over plain HTTP from another machine. Nightly backups go to
// CAFEDESK_BACKUP_DIR and to a bucket, CAFEDESK_BACKUP_S3_* (see
// backupDestinations).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/auth"
	"cafedesk/apps/server/internal/backup"
	"cafedesk/apps/server/internal/db"
	"cafedesk/apps/server/internal/server"
	"cafedesk/apps/server/internal/store"
	"cafedesk/apps/server/web"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(log)
	case "migrate":
		err = migrate(log)
	case "setup":
		err = setup(args)
	case "demo":
		err = demo(args)
	case "backup":
		err = backupCmd(args)
	case "restore":
		err = restoreCmd(args)
	default:
		err = fmt.Errorf("unknown command %q: use serve, migrate, setup, demo, backup or restore", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func openStore(ctx context.Context, log *slog.Logger) (*store.Store, func(), error) {
	pool, err := openPool(ctx, log)
	if err != nil {
		return nil, nil, err
	}
	return store.New(pool), pool.Close, nil
}

// openPool opens the database and migrates it.
func openPool(ctx context.Context, log *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	n, err := db.Migrate(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if n > 0 && log != nil {
		log.Info("migrated", "applied", n)
	}
	return pool, nil
}

// backupDestinations are where the nightly backups go, from the
// environment: a folder, CAFEDESK_BACKUP_DIR, and an S3-compatible bucket,
// CAFEDESK_BACKUP_S3_ENDPOINT, _BUCKET, _ACCESS_KEY_ID, _SECRET_ACCESS_KEY,
// _REGION (auto), _PREFIX (a folder in the bucket) and _PATH_STYLE=1 for
// buckets that need it.
func backupDestinations() ([]backup.Destination, error) {
	var dests []backup.Destination
	if dir := os.Getenv("CAFEDESK_BACKUP_DIR"); dir != "" {
		dests = append(dests, backup.Dir(dir))
	}
	env := func(k string) string { return os.Getenv("CAFEDESK_BACKUP_S3_" + k) }
	if env("ENDPOINT") != "" || env("BUCKET") != "" {
		region := env("REGION")
		if region == "" {
			region = "auto"
		}
		b, err := backup.NewBucket(backup.S3Config{Endpoint: env("ENDPOINT"), Bucket: env("BUCKET"),
			AccessKey: env("ACCESS_KEY_ID"), SecretKey: env("SECRET_ACCESS_KEY"), Region: region,
			PathStyle: env("PATH_STYLE") == "1", Prefix: env("PREFIX")})
		if err != nil {
			return nil, fmt.Errorf("backup bucket: %w", err)
		}
		dests = append(dests, b)
	}
	return dests, nil
}

// backupCmd writes a backup to a file, or with -now takes the nightly
// backup at once, to check its settings.
func backupCmd(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("o", "", "the file to write (default cafedesk-<date>.sql.gz here)")
	now := fs.Bool("now", false, "take the nightly backup now, to CAFEDESK_BACKUP_DIR and the bucket")
	fs.Parse(args)
	ctx := context.Background()
	pool, err := openPool(ctx, nil)
	if err != nil {
		return err
	}
	defer pool.Close()
	if *now {
		dests, err := backupDestinations()
		if err != nil {
			return err
		}
		if len(dests) == 0 {
			return errors.New("set CAFEDESK_BACKUP_DIR or CAFEDESK_BACKUP_S3_* first, or write a file with -o")
		}
		name, size, err := backup.Run(ctx, pool, dests, time.Now())
		if err != nil {
			return err
		}
		for _, d := range dests {
			fmt.Printf("Backed up to %s: %s, %d KB.\n", d, name, (size+1023)/1024)
		}
		return nil
	}
	if *out == "" {
		*out = "cafedesk-" + time.Now().Format("2006-01-02") + ".sql.gz"
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := backup.Write(ctx, pool, f, time.Now()); err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("Backed up to %s.\n", *out)
	return nil
}

// restoreCmd loads a backup into the new, empty database at DATABASE_URL.
func restoreCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("give the backup file: server restore cafedesk-2026-11-16.sql.gz")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	ctx := context.Background()
	// Not openPool: the backup's own schema version is made first.
	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	info, err := backup.Restore(ctx, pool, f)
	if err != nil {
		return err
	}
	fmt.Printf("Restored the backup taken %s. Start the server on this database.\n",
		info.Taken.Local().Format("2 Jan 2006 15:04"))
	return nil
}

func migrate(log *slog.Logger) error {
	_, closeDB, err := openStore(context.Background(), log)
	if err != nil {
		return err
	}
	closeDB()
	return nil
}

func serve(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, closeDB, err := openStore(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()

	port := os.Getenv("PORT") // Railway sets PORT.
	if port == "" {
		port = "8080"
	}
	handler := server.New(st, server.Config{
		Version:       version,
		SecureCookies: os.Getenv("CAFEDESK_INSECURE_COOKIES") != "1",
		TrustProxy:    os.Getenv("CAFEDESK_TRUST_PROXY") == "1",
		Admin:         web.Admin(),
		Log:           log,
	})
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	dests, err := backupDestinations()
	if err != nil {
		return err
	}
	if len(dests) == 0 {
		log.Warn("backups are off: set CAFEDESK_BACKUP_DIR or CAFEDESK_BACKUP_S3_* (docs/phase2.md)")
	} else {
		go backup.Nightly(ctx, st.Pool(), dests, log)
	}
	go func() {
		for range time.Tick(time.Hour) {
			if err := st.DeleteExpiredSessions(context.Background(), time.Now()); err != nil {
				log.Error("delete expired sessions", "err", err)
			}
		}
	}()
	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "version", version)
		errs <- srv.ListenAndServe()
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// setup creates the organization, its first shop and its owner, once.
func setup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	org := fs.String("org", "", "the organization's name, e.g. your company")
	shop := fs.String("shop", "", "the first shop's name, e.g. its area")
	name := fs.String("name", "", "the owner's name")
	username := fs.String("username", "", "the owner's username, to sign in to the admin")
	demo := fs.Bool("demo-menu", false, "start with the demo menu")
	fs.Parse(args)
	if *org == "" || *shop == "" || *name == "" || *username == "" {
		fs.Usage()
		return errors.New("give -org, -shop, -name and -username")
	}
	if !store.ValidUsername(store.NormalUsername(*username)) {
		return errors.New("a username is 2 to 32 letters, digits, dots, dashes or underscores, such as hampus")
	}
	// Everything that can fail without the password is checked before asking
	// for it: the database, and whether the server is set up already.
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, nil)
	if err != nil {
		return err
	}
	defer closeDB()
	if done, err := st.IsSetUp(ctx); err != nil {
		return err
	} else if done {
		return errors.New("this server is already set up; setup is only for a new database")
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	o, sh, _, err := st.Setup(ctx, *org, *shop, *name, *username, hash)
	if errors.Is(err, store.ErrConflict) {
		return errors.New("this server is already set up; setup is only for a new database")
	}
	if err != nil {
		return err
	}
	if *demo {
		if err := st.SeedDemoMenu(ctx, o.ID); err != nil {
			return fmt.Errorf("demo menu: %w", err)
		}
	}
	fmt.Printf("Set up %s with the shop %s. Sign in to the admin as %s.\n", o.Name, sh.Name, store.NormalUsername(*username))
	return nil
}

// demo fills a new database with a demo organization: the demo menu, three
// till staff, two tills and a month of made-up sales. It refuses a database
// that is set up, so it never mixes into a real shop's.
func demo(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	days := fs.Int("days", 30, "how many days of sales, up to today")
	fs.Parse(args)
	if *days < 1 || *days > 400 {
		return errors.New("-days is 1 to 400")
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, nil)
	if err != nil {
		return err
	}
	defer closeDB()
	if done, err := st.IsSetUp(ctx); err != nil {
		return err
	} else if done {
		return errors.New(`this database is set up already. The demo needs one of its own, empty, e.g.:
  docker compose exec postgres createdb -U cafedesk cafedesk_demo
then set DATABASE_URL to .../cafedesk_demo and run demo again`)
	}
	// A password of its own, shown once, unless one is given: a demo
	// database on a reachable server must not open with a known password.
	password := os.Getenv("CAFEDESK_OWNER_PASSWORD")
	if password == "" {
		token, _, err := auth.NewToken()
		if err != nil {
			return err
		}
		password = token[:16]
	}
	if err := auth.CheckPasswordStrength(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	const username = "demo"
	o, sh, owner, err := st.Setup(ctx, "Demo café", "Demo shop", "Demo owner", username, hash)
	if err != nil {
		return err
	}
	if err := st.SeedDemoMenu(ctx, o.ID); err != nil {
		return fmt.Errorf("demo menu: %w", err)
	}
	if err := st.SeedDemoCosts(ctx, o.ID); err != nil {
		return fmt.Errorf("demo costs: %w", err)
	}
	people := []struct {
		name string
		role api.StaffRole
		pin  string
	}{{"Ravi", api.StaffRoleManager, "5678"}, {"Anil", api.StaffRoleCashier, "2468"}, {"Priya", api.StaffRoleCashier, "1357"}}
	var manager store.DemoStaff
	var cashiers []store.DemoStaff
	for _, p := range people {
		s, err := st.CreateStaff(ctx, o.ID, api.StaffInput{Name: p.name, Role: p.role, ShopIds: []uuid.UUID{sh.ID},
			Active: true}, auth.HashPIN(p.pin))
		if err != nil {
			return fmt.Errorf("demo staff: %w", err)
		}
		if p.role == api.StaffRoleManager {
			manager = store.DemoStaff{ID: s.Id, Name: s.Name}
		} else {
			cashiers = append(cashiers, store.DemoStaff{ID: s.Id, Name: s.Name})
		}
	}
	if err := st.SeedDemoSales(ctx, o.ID, sh.ID, manager, cashiers, *days, time.Now(), 1); err != nil {
		return fmt.Errorf("demo sales: %w", err)
	}
	if err := st.SeedDemoPurchases(ctx, o.ID, sh.ID, owner.ID, *days, time.Now(), 1); err != nil {
		return fmt.Errorf("demo purchases: %w", err)
	}
	if err := st.SeedDemoIngredients(ctx, o.ID); err != nil {
		return fmt.Errorf("demo ingredients: %w", err)
	}
	if err := st.SeedDemoRecipes(ctx, o.ID); err != nil {
		return fmt.Errorf("demo recipes: %w", err)
	}
	fmt.Printf(`Set up %s with %d days of made-up sales and purchases.
Sign in to the admin as %s with the password %s
Starting PINs for a till: Ravi (manager) 5678, Anil 2468, Priya 1357.
`, o.Name, *days, username, password)
	return nil
}

// readPassword asks for the owner's password twice on a terminal, or takes
// it from CAFEDESK_OWNER_PASSWORD.
func readPassword() (string, error) {
	if pw := os.Getenv("CAFEDESK_OWNER_PASSWORD"); pw != "" {
		return pw, auth.CheckPasswordStrength(pw)
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("run setup in a terminal to type the password, or set CAFEDESK_OWNER_PASSWORD")
	}
	fmt.Printf("Password for the admin (at least %d characters): ", auth.MinPasswordLength)
	first, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	if err := auth.CheckPasswordStrength(string(first)); err != nil {
		return "", err
	}
	fmt.Print("The same password again: ")
	second, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("the two passwords were different")
	}
	return string(first), nil
}
