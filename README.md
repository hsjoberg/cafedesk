# Cafedesk

A simple point of sale for small shops, cafés and cafeterias: a touch-screen app
on the shop's Windows PC that works offline, a Go server with PostgreSQL that it
syncs to (on Railway, or on a machine in the shop), and a web admin.

## The till

Taking an order: a tap per item, options such as sugar or spice, and the
total to charge.

![The till taking an order](docs/screenshots/till-order.png)

The day report, with the cash counted against what the drawer should hold.

![The till's day report](docs/screenshots/till-day.png)

Receipts print in English and Arabic, drawn as a picture so any ESC/POS
receipt printer can print the Arabic.

<img src="docs/screenshots/receipt.png" alt="A receipt in English and Arabic" width="360">

## The admin

Sales by day or hour, profit from what the items cost, and the tills' cash
counts.

![The admin's Sales page](docs/screenshots/admin-sales.png)

The menu, in English and Arabic, laid out as the till shows it.

![The admin's Menu page](docs/screenshots/admin-menu.png)

Purchases from invoices, set against sales.

![The admin's Purchases page](docs/screenshots/admin-purchases.png)

The shop's details, as receipts print them.

![The admin's Shop page](docs/screenshots/admin-shop.png)

The screenshots are of a demo with made-up data (`go run . demo` in
`apps/server`).

- [PLAN.md](PLAN.md): the plan, decisions and phases.
- [docs/design.md](docs/design.md): the POS's visual design.
- [docs/phase0.md](docs/phase0.md): the checks to run on the POS PC.
- [docs/phase1.md](docs/phase1.md): using the till: sign-in, selling, orders, closing the day, staff, backups.
- [docs/phase2.md](docs/phase2.md): the server and the admin: running them, security, deploying.

## Layout

```
api/openapi.yaml   the API contract
pkg/               shared Go: money
apps/pos/          the POS: MyGo native UI, SQLite, printing
apps/server/       the Go server: API, Postgres, and the admin built into it
apps/admin/        the Solid admin: menu management
docs/              design and run-books
railway.toml       Railway's build and health check settings
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

# The server and the admin (details in docs/phase2.md)
docker compose up -d postgres
cd apps/server
export DATABASE_URL='postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable'
go run . setup -org "..." -shop "..." -name "..." -username "..." -currency AED -timezone Asia/Dubai   # once
go run .              # http://localhost:8080
TEST_DATABASE_URL="$DATABASE_URL" go test ./...
cd ../admin && bun install && bun run dev   # http://localhost:5173

# Postgres, and the server with the admin, in Docker
docker compose up --build
```

A dev container (`.devcontainer/`) runs Go, Node and Postgres for the server
and the admin. The POS is a Windows GUI app, so build and run it on the host.

## The POS's data

On Windows, the POS keeps `config.json` (server address, printer, full screen)
and `pos.db` (its SQLite database) in `%APPDATA%\Cafedesk POS`.
