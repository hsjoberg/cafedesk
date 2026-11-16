// Command server is the Cafedesk server: the API of api/openapi.yaml and
// the admin, on PostgreSQL.
//
//	server                 serve (migrating the database first)
//	server migrate         migrate the database and exit
//	server setup -org ...  create the organization, its first shop and owner
//
// Environment: DATABASE_URL (required), PORT (8080), CAFEDESK_TRUST_PROXY=1
// behind a proxy such as Railway's, CAFEDESK_INSECURE_COOKIES=1 only for
// development over plain HTTP from another machine.
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

	"cafedesk/apps/server/internal/auth"
	"cafedesk/apps/server/internal/db"
	"cafedesk/apps/server/internal/server"
	"cafedesk/apps/server/internal/store"
	"cafedesk/apps/server/web"

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
	default:
		err = fmt.Errorf("unknown command %q: use serve, migrate or setup", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func openStore(ctx context.Context, log *slog.Logger) (*store.Store, func(), error) {
	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, nil, err
	}
	n, err := db.Migrate(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	if n > 0 && log != nil {
		log.Info("migrated", "applied", n)
	}
	return store.New(pool), pool.Close, nil
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
	email := fs.String("email", "", "the owner's email, to sign in to the admin")
	demo := fs.Bool("demo-menu", false, "start with the demo menu")
	fs.Parse(args)
	if *org == "" || *shop == "" || *name == "" || *email == "" || !strings.Contains(*email, "@") {
		fs.Usage()
		return errors.New("give -org, -shop, -name and -email")
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
	o, sh, _, err := st.Setup(ctx, *org, *shop, *name, *email, hash)
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
	fmt.Printf("Set up %s with the shop %s. Sign in to the admin as %s.\n", o.Name, sh.Name, *email)
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
