# Phase 0: checks on the POS PC

Phase 0 answers one question: can this stack run the shop on the real POS PC?
The app has a **Checks** screen (top right of the order screen) that tests each
part. Run through it on the POS PC and note the results in the table at the end.

## 1. Start a server for the POS to call

**Locally (enough for now):** on your development PC, run `go run .` in
`apps/server`, and allow it through Windows Firewall when asked. Find the PC's
address with `ipconfig` (for example `192.168.1.20`). The POS's server address is
then `http://192.168.1.20:8080`. This only tests the local network, not the
internet, but that's all Phase 0 needs until the shop goes live.

**On Railway (later, when the shop goes live):**

1. Create a project, and add a service from this repository (push it to GitHub
   first), or deploy from your machine with the Railway CLI: `railway up` in
   `apps/server`.
2. Leave the service's **root directory** as the repository root. Railway then
   builds `apps/server/Dockerfile`, as `railway.toml` says, and checks
   `/healthz`. (See [phase2.md](phase2.md) for the database and setup.)
3. Under **Networking**, choose **Generate domain**. You get an address like
   `https://cafedesk-server-production.up.railway.app`, with HTTPS set up by
   Railway.
4. Open `<that address>/v1/ping` in a browser. It should show
   `{"status":"ok",...}`.

No database is needed yet; Postgres arrives in Phase 2.

## 2. Install the POS app

1. Copy `apps/pos/build/windows-amd64/cafedesk-pos-setup-0.1.0-amd64.exe` to the
   POS PC (USB stick is fine) and run it.
2. The installer isn't signed yet, so Windows SmartScreen will warn. Choose
   **More info**, then **Run anyway**. Signing comes before the app is used for
   real.
3. Open **Cafedesk POS**. Native UI needs no WebView2; it draws with Direct3D 11,
   or with Windows' software renderer if the graphics driver can't.

To point the app at your server, close it, edit
`%APPDATA%\Cafedesk POS\config.json`, set `"serverUrl"` to your Railway address,
and open it again.

To rebuild the installer after changes: `go tool mygo build` in `apps/pos`.

## 3. Run the checks

| Check | How | Passes when |
|---|---|---|
| Touch | Put two fingers on the box together. | "Most fingers at once" reaches 2 or more. |
| Pointer | Read the line under the touch box. | It says the system reports a touch screen. |
| Scrolling | Drag the test list with one finger, then drag the Checks screen itself. On the order screen, add 12 or more different items and drag the receipt. | Each scrolls with one finger, and a drag on the receipt never selects a line. (MyGo's own touch scrolling doesn't work on Windows yet, issue #1712; this is our workaround.) |
| Arabic | Look at the sample. | Letters join up and lines read right to left. |
| Order screen | Ring up an order: tiles, Next and Previous, a line's − and +, Charge, a quick cash amount, Complete sale. | Every tap lands first time, and nothing feels slow. |
| Receipt printer | Choose the receipt printer, keep **Receipt printer codes**, press **Print test receipt**. If nothing prints or it prints garbage, choose **Windows driver** and try again. | A readable receipt prints, and with printer codes, the paper is cut. |
| Kitchen printer | If it's connected, choose it and print a test the same way. Then choose the receipt printer again. | Same as above. |
| Database | Press **Write and read a row**. Restart the PC with the network cable out, open the app, and press it again. | The row number goes up across the restart. |
| Server | Press **Call the server**. | "Reached in … ms", and the clocks differ by a few seconds at most. |
| Start-up | Turn on **Start when Windows starts**, then restart the PC. | The app opens by itself after sign-in. |
| Full screen | Turn on **Full screen**. | The app fills the screen, and stays so after a restart. |

The app saves sales from the order screen to its database for real, so
Phase 0 orders show up in the database check's counts. They are test data;
Phase 1 starts a fresh database.

## 4. Results

Fill this in on the POS PC:

| Check | Result | Notes |
|---|---|---|
| Touch (most fingers) | | |
| Pointer reports touch | | |
| One-finger scroll | | |
| Two-finger scroll | | |
| Arabic | | |
| Order screen feel | | |
| Receipt printer: model, mode that worked | | |
| Kitchen printer: model, mode that worked | | |
| Database across restart | | |
| Server latency and clock difference | | |
| Start at login | | |
| Full screen | | |
| POS PC: screen size and resolution, Windows 10 edition and build | | |

## What was verified on the development machine

- All tests pass in `pkg`, `apps/server` and `apps/pos`. The POS tests drive
  the screens without a window: they ring up an order, take cash, check the
  change, and confirm the order and its outbox row are saved in SQLite on
  disk.
- The packaged app starts, and creates its settings and a WAL-mode database
  in `%APPDATA%\Cafedesk POS`.
- The server answers `/healthz` and `/v1/ping` when run locally.
- **Not verified:** the server's Docker image. Docker Desktop wasn't running
  on the development machine, so `docker build apps/server` hasn't been run
  yet. Railway runs this build when you deploy, so a mistake there will show
  in its build log.
