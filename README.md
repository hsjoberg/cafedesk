# Cafedesk

A simple point of sale for small shops, cafés and cafeterias: a touch-screen app
on the shop's Windows PC that works offline, a Go server with PostgreSQL that it
syncs to (on Railway, or on a machine in the shop), and a web admin (later).

- [PLAN.md](PLAN.md): the plan, decisions and phases.
- [docs/design.md](docs/design.md): the POS's visual design.
- [docs/phase0.md](docs/phase0.md): the checks to run on the POS PC.
- [docs/phase1.md](docs/phase1.md): using the till: sign-in, selling, orders, closing the day, staff, backups.

## Layout

```
api/openapi.yaml   the API contract
pkg/               shared Go: money
apps/pos/          the POS: MyGo native UI, SQLite, printing
apps/server/       the Go server (Railway)
apps/admin/        the Solid admin (Phase 2, not started)
docs/              design and run-books
```

`go.work` ties the Go modules together for development.

## Develop

Needs Go 1.27.4 or later; with `GOTOOLCHAIN=auto` (Go's default) an older Go
downloads it by itself.

```sh
# The POS (on Windows)
cd apps/pos
go tool mygo dev      # runs the app, rebuilding on save
go test ./...         # tests the screens without a window
go tool mygo build    # builds build/windows-amd64/cafedesk-pos-setup-*.exe

# Render the screens to PNGs for design review
CAFEDESK_SNAPSHOTS=testdata/snapshots go test -run TestSnapshots ./...

# The server
cd apps/server
go run .              # http://localhost:8080/v1/ping
go test ./...

# Postgres, and the server in Docker
docker compose up --build
```

A dev container (`.devcontainer/`) runs Go, Node and Postgres for the server
and the admin. The POS is a Windows GUI app, so build and run it on the host.

## The POS's data

On Windows, the POS keeps `config.json` (server address, printer, full screen)
and `pos.db` (its SQLite database) in `%APPDATA%\Cafedesk POS`.
