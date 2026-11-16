# Cafedesk: plan

A simple, own-built POS for a karak and meals cafeteria in Dubai.
Last updated: 2026-11-16.

## 1. Goals

- A touch-screen POS on the shop's Windows 10 PC: big item buttons, a cart, payment, receipt printing.
- Works **offline**; syncs to the cloud when online.
- Later: per-item **margins** (ingredient and wholesale costs) and sales **statistics**.
- A **web admin** for menu, prices, costs and reports.
- Windows 10 first. macOS and Linux kept possible, not a priority.
- Deliberately simple. No off-the-shelf POS (they are too complex).

## 2. Decisions

| Area | Decision |
|---|---|
| POS UI | **MyGo native UI** (Go). No web view in the POS. |
| POS local data | SQLite on the POS PC (MyGo SQLite plugin, or another Go SQLite if Phase 0 says so) |
| Server | One **Go** service + **PostgreSQL**, hosted on **Railway** (managed Postgres) |
| Server URL / TLS | Railway-provided `*.up.railway.app` HTTPS domain. Custom domain optional later. Base URL is a config setting, never hard-coded. |
| API | **REST/JSON described by an OpenAPI spec** (write as 3.1; consider 3.2 later for streams). **No gRPC**: Railway's public edge talks HTTP/1.1 to containers, which breaks native gRPC. |
| Admin | **Solid** web app, served by the same Go binary |
| Dev environment | Dev container for server, admin and Postgres. The POS app is a Windows GUI app and is built and run on the host. |
| Where it runs | **Development is always local:** Postgres via `docker compose`, the server with `go run`, the POS pointed at `http://localhost:8080`. **Production deploys to Railway.** **Fully local production stays a supported option:** server and Postgres on a machine in the shop (the POS PC itself or a second one), reached over the LAN. The code is the same for every setup, and nothing may depend on Railway. |
| Tenancy | `org_id` on every table; `shop_id` on every shop-scoped table. One org (the company) and one shop to begin with. |
| Terminals | Multiple terminals per shop are supported in the model. Today: **one terminal**. |
| Printers | **Two network printers**: a receipt printer at the desk, and a kitchen printer (currently not connected). Printing goes through the Windows print API. |
| VAT | **Not registered today**, so no VAT on receipts. Built in but switched off. |
| If MyGo is abandoned | Rewrite the POS in **Qt/C++** against the same server API. No local Go layer. |
| Streams (future) | **SSE** carrying a small "something changed" nudge. Polling stays as the fallback. |

## 3. Architecture

```
POS PC (Windows 10)                         Railway
┌────────────────────────────┐            ┌──────────────────────────┐
│ MyGo native UI             │   HTTPS    │ Go service               │
│ local SQLite (source of    │ ─────────► │  ├─ REST API (OpenAPI)   │
│   truth until synced)      │ ◄───────── │  ├─ services / domain    │
│ outbox + sync worker       │            │  ├─ Postgres store       │
│ print queue → spooler      │            │  └─ serves Solid admin   │
└────────────────────────────┘            └────────────┬─────────────┘
                                                       │
                                                  PostgreSQL
                                                  (private network)
```

Repo layout:

```
cafedesk/
├─ api/openapi.yaml    the contract (sync + admin)
├─ docs/sync.md        written sync protocol
├─ pkg/                shared Go: money, tax calculation (pure functions)
├─ testdata/           shared test vectors (orders → totals, tax, rounding)
├─ apps/pos/           MyGo native UI + SQLite + outbox + print + sync
├─ apps/server/        Go service + Postgres (sqlc, pgx, migrations)
├─ apps/admin/         Solid admin
├─ .devcontainer/
└─ docker-compose.yml  Postgres for development
```

### The POS is local-first
- Every sale is written to local SQLite and printed. No network is involved.
- Each sale also writes an `outbox` row. A background worker uploads unsent rows when online.
- The server confirms receipt, and only then does the POS mark rows as sent.
- Menu and price changes made in the admin are pulled down by the POS.
- **Keep the POS thin.** It only does: read the menu, build a cart, compute totals and tax, store orders, print, sync. All reports, margins and statistics are computed on the server. This keeps a possible Qt rewrite small.

### Sync protocol (written down in `docs/sync.md`)
- **Up:** orders, payments, voids and shift closes are append-only. Each has a client-generated **UUIDv7**. The server upserts by ID, so retries are safe.
- **Down:** each catalog or config change bumps a per-org version number. The POS asks for "everything since version N".
- **Conflicts:** normally none. Orders are append-only, and the server owns the menu.
- **Snapshot at sale time:** each order line stores the price, cost, tax rate and tax amount that applied then. Later changes don't rewrite history.
- Versioned paths (`/v1/...`). Device token in the `Authorization` header, scoped to one shop.
- Money is stored as **integer fils**. Timestamps are UTC.
- Unique per-shop **order numbers** per business day, with a terminal prefix.

### Terminal pairing
Admin generates a short code. The terminal operator enters it on the touch numpad. The terminal receives a device token scoped to its shop. There are no passwords on the POS.

### Server layers
API handlers → services (catalog, order ingest, costing and margins, reports) → store (`sqlc` + `pgx`, versioned migrations). Scheduled jobs run inside the binary, with no message queue. Reports are plain SQL.

### Auth
- Terminals: device tokens from pairing.
- Admin users: email and password (Argon2) or passkeys, with roles per org and per shop.
- Rate limiting on login and pairing. The server URL is public, so authentication is the protection.

### Data model (sketch)

| Level | Tables | Keys |
|---|---|---|
| Org-wide | items, categories, recipes, ingredients, users | `org_id` |
| Shop-scoped | terminals, printers, orders, order lines, payments, shifts, print jobs | `org_id`, `shop_id` |
| Later | per-shop menu overrides (price, availability) | `org_id`, `shop_id` |

`shop_id` is repeated on order lines and payments so per-shop reports are simple queries.

## 4. Printing

- The POS prints through the **Windows spooler**, called from Go (`x/sys/windows`, no cgo).
- If a printer is an ESC/POS thermal printer, send raw ESC/POS bytes (small, crisp, paper cut, cash-drawer kick possible). If it is a regular graphics printer, render the receipt as an image or PDF. **Phase 0 settles which for each printer.**
- Printers are a setting: name or address, ticket type, enabled flag.
- **Routing rules** decide which items or categories go to which printer:
  - The receipt printer gets the customer receipt.
  - The kitchen printer gets a **kitchen ticket**: order number, items with modifiers, time, no prices.
- **Printing never blocks a sale.** Each print is a job in a local queue with retry. The screen shows "kitchen printer offline, reprint" when needed. Manual **reprint** exists for both receipts and tickets.
- The kitchen printer is **off by default**. Enabling it later needs no code change.

## 5. VAT (off today, built in)

- Not VAT-registered, so receipts must **not** show VAT today.
- A single **tax setting** per shop, off by default, with a TRN field.
- Tax rate per item. Setting for whether prices include tax.
- Each order line snapshots its rate and tax amount (0 while unregistered).
- Two receipt templates: **Receipt** and **Tax Invoice** (TRN, sequential invoice numbers, VAT breakdown), chosen by the setting.
- A stats tile tracking **rolling 12-month turnover** against the mandatory registration threshold (AED 375,000 at the time of writing; confirm with the accountant).
- Check with the accountant about UAE e-invoicing timing and applicability.

## 6. POS UI notes (MyGo native)

Known gaps found in the docs (MyGo v0.9.1, pre-1.0):

| Gap | Answer |
|---|---|
| Finger drags don't scroll on Windows (issue #1712; fix PR #1911 still open on 2026-11-16). **Tested on a Windows 10 LTSC 2019 (1809) POS: neither one nor two fingers scroll**; only dragging the scrollbar does. Multi-touch itself works. | Main screens are **paged grids** with Next/Prev, so they don't scroll. Lists that must scroll (the receipt, the Checks screen) use our own one-finger drag, `touchScroll` in `apps/pos/touchscroll.go`, built on MyGo's raw touches; it also ignores the click Windows sends as a drag ends. Remove it once MyGo ships the fix. |
| No documented on-screen keyboard | Build our own **numpad** (PIN, cash tendered, quantity). A small keyboard later if notes need it. |
| No printing API in native UI | Print from Go through the Windows spooler |
| No documented kiosk mode; the Windows key still opens Start | Use a Windows feature such as **Assigned Access** to lock the machine to the app |

Also to confirm: renders on the PC's graphics (Direct3D 11, with a WARP software fallback), Arabic and RTL text, autostart at login (`SetOpenAtLogin`; Task Scheduler if admin rights are needed), recovery after a power cut.

Pin the MyGo version. Keep business logic in plain packages so the UI layer stays replaceable.

## 7. Phases

### Phase 0: Feasibility spike (1–2 days)
**Status (2026-11-16):** built and tested on the development machine. The
checks on the POS PC are next: see [docs/phase0.md](docs/phase0.md).

Pass or fail on the real POS PC:
- [ ] MyGo native hello-world: touch button grid with paging, a numpad, Arabic text, correct rendering, autostart at login.
- [ ] Touch panel: does it report **two or more contact points** (needed for two-finger scroll)? Test one-finger drag and two-finger scroll in a `ui.Scroll` list.
- [ ] SQLite write and read across a reboot, with no internet.
- [ ] Test print through the Windows spooler to the receipt printer, and the kitchen printer if connected. Identify whether each is ESC/POS.
- [ ] A tiny Go HTTPS endpoint on Railway, called from the POS PC. Confirm the generated domain serves HTTPS.
- [ ] Repo, dev container, Postgres in Docker compose.

If native UI fails (touch, graphics or text), options are: wait for the MyGo fix, or fall back to Qt/C++.

### Phase 1: POS MVP, local only (about 2–3 weeks)
**Status (2026-11-16):** built and tested on the development machine; to be
tried on the POS PC. See [docs/phase1.md](docs/phase1.md). Beyond the list
below, it also has business days that run past midnight, the menu (with
modifiers) stored in the local database ready for Phase 2's sync, and a
manager-only Settings screen with an on-screen keyboard.

- Category tabs with big item buttons, a cart, quantity changes, modifiers (for example sugar level, extra shot).
- Cash and card payments (card recorded only, no terminal integration).
- Receipt printing and reprint; kitchen ticket support, off by default.
- Voids, refunds, staff and manager PIN, end-of-day close with a Z report.
- SQLite in WAL mode, automatic local backups to a second folder or USB drive.
- Run in the shop alongside the current way of working.

### Phase 2: Server, sync and admin (about 2–3 weeks)
- Go API, Postgres schema and migrations, OpenAPI spec, device pairing.
- Outbox sync with retry; online/offline indicator on the POS.
- Admin: login, menu and price management, order list, daily sales.
- Backups (Railway's, plus nightly `pg_dump` to object storage), uptime alert, alert when a terminal has not synced for 24 hours.

### Phase 3: Costs and margins (about 2 weeks)
1. Simple: one manual cost per item, giving margin per item.
2. Proper: ingredients with purchase records (supplier, quantity, price), recipes (a cup of karak = milk + tea + sugar + cardamom + cup), cost per unit (rolling average or latest), cost per item from the recipe, snapshotted on each sale.

### Phase 4: Statistics (about 1–2 weeks)
Sales by day, hour, item and category; margin per item and per day; best and worst sellers; low-margin warnings; optional daily or weekly summary by WhatsApp or email.

### Phase 5: Hardening and later
- POS auto-update (MyGo supports delta updates).
- Fully local production setup: the server as a Windows service, Postgres on Windows (installer or bundled), a LAN firewall rule, HTTPS on the LAN (Tailscale, or a certificate the POS trusts), and nightly backups to another disk.
- macOS and Linux builds.
- Stock counts and waste tracking.
- A second terminal, or a kitchen display as another terminal role.
- Streaming via SSE for instant menu updates and live dashboards.

## 8. Risks

| Risk | Mitigation |
|---|---|
| MyGo is pre-1.0 with frequent breaking changes | Pin the version, keep logic in plain packages, Phase 0 test. Fallback: rewrite the POS in Qt/C++ against the same API. |
| Native UI touch scrolling bug | Paged grids; recheck the fix in Phase 0 |
| No on-screen keyboard | Custom numpad |
| Losing sales data on a crash or power cut | SQLite WAL, local backups, outbox cleared only after server confirmation |
| POS fails during service | Keep the old way of taking orders as a fallback for the first weeks |
| Windows 10 is out of support (October 2025) | Keep the PC locked down; consider extended security updates or Windows 11 |
| A Qt rewrite duplicates POS logic | Thin POS, reports on the server, shared test vectors in `testdata/` |
| Contract drift between POS, server and admin | OpenAPI is the source of truth, with generated clients; `docs/sync.md` |

## 9. Streaming later

- The OpenAPI **3.2** spec can describe one-way streams (SSE `text/event-stream`, JSONL) with `itemSchema`. Tooling support is uneven, so write the spec as 3.1 now.
- Design: the stream carries only a nudge such as `{"catalogVersion": 42}`; the POS then does its normal pull. Polling (every 30–60 s) remains the fallback.
- WebSockets are not covered by OpenAPI (a separate spec such as AsyncAPI would be needed). We don't expect to need them.
- Connections may be capped by Railway, so clients must reconnect and resume. This is not yet verified.

## 10. Open questions

1. Models of the **receipt printer** and the **kitchen printer**; whether each is ESC/POS; whether the kitchen printer works when plugged in.
2. POS screen size and resolution; GPU/driver.
3. Whether Arabic item names or receipts are needed.
4. Card payments: recorded only, or integrated with a terminal later?
5. Does Railway's current plan and region suit the shop's latency and budget? Check pricing before committing.
6. Backup destination for nightly dumps (object storage choice).
