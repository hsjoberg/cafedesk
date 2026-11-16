# Phase 2: the server and the admin

Phase 2 builds the server and the admin in steps. This page covers what exists
now and how to run it.

| Step | What | State |
|---|---|---|
| 1 | Server foundation: Postgres schema and migrations, setup command, admin sign-in | Done |
| 2 | Menu management in the admin: categories, items, option groups | Done |
| 3 | Connecting tills | Done |
| 4 | Menu and staff down to the tills; till staff managed in the admin | Done |
| 5 | Sales up from the tills: days, orders, voids, refunds, PIN changes | Done |
| 6 | Reports in the admin | Next |

## Connect a till

A till is always connected to an admin: there is no standalone mode. Connect
it once, the first time it starts. After that it works offline as well as
online.

1. In the admin, on **Staff**, add at least one **manager** for the shop, with a
   starting PIN. The admin won't add a till until its shop has one, so someone
   can sign in to it.
2. On **Tills**, choose **Add a till**. It is named T1, T2… in order, and its
   order numbers start with that name. The admin shows a code of 8 digits, which
   works once, for 10 minutes.
3. Start the till. It shows **Connect this till to your admin**: set the admin's
   address (`https://…` on Railway, or `http://<the PC's address>:8080` for a
   server on the shop's network), then type the code.
4. The till fetches the menu and staff, then shows the sign-in screen. **Sign in
   with the starting PIN from the admin**; the till then asks the person to
   choose their own.

The till keeps its connection key in Windows Credential Manager, not in
`config.json`. If a till has sales from being tried out (in demo mode, or from
before this version), connecting sets them aside in `pos-demo-<date>.db` next to
its database, so they never reach the admin's reports.

To connect a till again, as after it was disconnected in the admin or its PC was
replaced: in the admin, show a new code for it on **Tills**; on the till, open
**Settings**, **Admin connection**, **Connect again**.

**Demo mode**, for trying the till without an admin, is a setting for
developers: start the app with `CAFEDESK_DEMO=1`. It has the demo menu and a
manager whose starting PIN is 1234.

## How syncing works

- **Every 30 seconds**, and after each sale, void, refund, start or close of the
  day, and PIN change, the till pulls what changed since its last sync, then
  sends what waits in its outbox, oldest first.
- **Offline, the till works as before.** Its sales wait in the outbox and go up
  when the connection is back. "Offline" shows by the clock while it can't
  reach the admin, and the till menu says when it was last up to date and how
  many changes wait.
- **Sending twice changes nothing.** The server takes each event once, by its
  IDs.
- **If the server turns an event away**, as for an order of a day it never got,
  sending stops there, and "Not sending" shows by the clock, with the reason in
  the till menu and in Settings. The events wait until it is sorted.
- **Disconnecting** a till in the admin ends its key at once. The till shows
  "Disconnected" and keeps selling, and its sales wait until it is connected
  again.
- **Staff** come from the admin, and the till's Settings shows them read-only. A starting PIN set in the admin wins over a PIN chosen at a
  till in the meantime: the till sends the PIN it replaced, and the server only
  takes the change if that is still the current one.

## Run it locally

You need Docker (for Postgres), Go and Bun.

```sh
# 1. Postgres
docker compose up -d postgres

# 2. Set up your organization, first shop and owner login (once).
#    It asks for a password twice. -demo-menu starts with the till's demo menu.
cd apps/server
export DATABASE_URL='postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable'
go run . setup -org "Your company" -shop "Downtown" -name "Your name" -email you@example.com -demo-menu

# 3. The server, on http://localhost:8080
go run .
```

The admin is served by the server once it is built into it:

```sh
cd apps/admin
bun install
bun run build       # writes into apps/server/web/dist; restart the server
```

While working on the admin, run Vite instead, which reloads on save and
passes the API to the server:

```sh
cd apps/admin
bun run dev         # http://localhost:5173
```

On Windows, in PowerShell:

```powershell
docker compose up -d postgres
cd apps\server
$env:DATABASE_URL = 'postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable'
go run . setup -org "Your company" -shop "Downtown" -name "Your name" -email you@example.com -demo-menu
cd ..\admin
bun install
bun run build
cd ..\server
go run .
```

`$env:DATABASE_URL` lasts only for that window. In an MSYS2 terminal the bash
commands above work, but the password prompt may not; then set
`CAFEDESK_OWNER_PASSWORD` for the `setup` run instead.

## The admin

- **Menu**: category tabs and tiles, drawn as the till draws them. Click a tile
  to edit it on the right; the tile changes as you type. Add, edit, reorder and
  delete categories and items. Choose a category's band color, which option
  groups an item has, and whether it prints on the kitchen ticket.
- **Options**: option groups such as Sugar or Extras. "One" (a sugar level) or
  "Any number" (extras), each option with an Arabic name, a price change, and
  whether it is chosen when the item is tapped.
- On a phone, the editor opens as a full-screen sheet.

Deleting archives rather than erases, so that terminals hear about it when they
sync. A category can only be deleted once it is empty. Deleting an option group
takes it off the items that had it.

Every change takes the organization's **catalog version** up by one, and marks
the rows it changed with it. A terminal holding version N will ask only for
rows above N (step 4).

## Security

- **Admin passwords** are hashed with Argon2id (19 MiB, 2 passes) and are at
  least 10 characters. Ten failed sign-ins for an email from one address lock
  it out for 15 minutes. A wrong email takes as long to answer as a wrong
  password.
- **Sessions** are a random 32-byte token in a cookie that is `HttpOnly`,
  `Secure` and `SameSite=Lax`, valid for 14 days. The database keeps only the
  token's SHA-256, so a stolen database signs no one in. Signing out deletes
  the session.
- **Requests that change data** must be JSON and come from the admin's own
  origin. A page on another site can't make them with your cookie.
- **Every query** is scoped to the organization of the session, never one sent
  in the request, so organizations can't see or change each other's data.
- Headers keep the admin from being framed or loading anything from other
  sites.

Server environment variables:

| Variable | Meaning |
|---|---|
| `DATABASE_URL` | Postgres connection string. Required. |
| `PORT` | Port to listen on; Railway sets it. Default 8080. |
| `CAFEDESK_TRUST_PROXY=1` | Take the client's address from `X-Forwarded-For`. Set it on Railway; leave it off anywhere else, as anyone could fake the header. |
| `CAFEDESK_INSECURE_COOKIES=1` | Never mark the session cookie HTTPS-only. Not needed for a server in the shop: over plain HTTP to a local address (`localhost`, `192.168.…`, `10.…`) the server already leaves it off, and to a public address, as on Railway, it always sets it. |
| `CAFEDESK_OWNER_PASSWORD` | Lets `setup` run without a terminal. |

## Tests

```sh
cd apps/server
TEST_DATABASE_URL='postgres://cafedesk:cafedesk@localhost:5432/cafedesk?sslmode=disable' go test ./...
```

Each test makes a database of its own on that server and drops it at the end.
Without `TEST_DATABASE_URL` the database tests are skipped.

```sh
cd apps/admin
bun run typecheck
```

The till's sync is tested against a fake server in `apps/pos/sync_test.go`.
`apps/pos/e2e_test.go` runs a till against a real server: set
`CAFEDESK_E2E_SERVER` to its address, `CAFEDESK_E2E_CODE` to a till's code, and
`CAFEDESK_E2E_PIN` to a manager's starting PIN, then `go test -run TestEndToEnd`.

## The API contract

`api/openapi.yaml` is the source of truth. After changing it:

```sh
cd apps/server && go generate ./...   # Go types and handler interface
cd apps/admin && bun run gen:api      # TypeScript types
```

## Deploying to Railway (when you're ready)

1. In Railway, add a **PostgreSQL** service and a service from this repository.
   Leave the server's **root directory** as the repository root:
   `railway.toml` points Railway at `apps/server/Dockerfile`, which builds the
   admin and the server together.
2. On the server service, set `DATABASE_URL` to the Postgres service's
   **private** URL (a reference variable such as `${{Postgres.DATABASE_URL}}`),
   and `CAFEDESK_TRUST_PROXY=1`. Leave Postgres's public networking off.
3. Generate a domain for the server.
4. Run `setup` once against the Railway database. The simplest way is to turn
   on Postgres's public networking for a minute, run `go run . setup ...` from
   your machine with that public URL, then turn it off again.
