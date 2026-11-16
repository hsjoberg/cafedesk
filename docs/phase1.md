# Phase 1: the till

What the POS does now, and how to try it. Everything works offline; nothing
syncs yet (Phase 2).

## First run

1. Open **Cafedesk POS**. If a Phase 0 database is there, it is renamed to
   `pos-phase0-<date>.db` in `%APPDATA%\Cafedesk POS` and a fresh one starts.
2. The till opens locked. Sign in with **1234**: that's the **Owner**, a
   manager. The till then makes you choose your own PIN, typed twice.
3. **Start the day**: count the cash in the drawer, type it, tap
   **Start the day**.

## Selling

- Tap a tile to add the item. A plain karak stays one tap: items get their
  default options (normal sugar, normal spice).
- Tap a line on the receipt for **−**, **+**, **Options** and **Remove**.
  **Options** changes sugar, spice or extras for that line. A line with
  different options is a separate line.
- **Charge**, then cash (type the amount, or tap a quick amount) or card,
  then **Complete sale**. The receipt prints. If the kitchen printer is on,
  meals and snacks print a kitchen ticket.
- A print that fails never stops a sale. It shows at the top of the receipt
  with **Retry**. Kitchen tickets are retried by themselves a few times
  first.

## The till menu

Tap your name at the top right:

| Option | What it does | Who |
|---|---|---|
| Orders | The latest orders, each shown as its receipt. **Reprint**, **Refund** some or all of its items, or **Void** it. | Anyone; refunds and voids need a manager's PIN |
| Day report | Sales so far. **Print day report** prints an X report. **Close the day**: count the cash, see if it's short or over, close. The Z report prints and a backup is made. | Closing needs a manager |
| Settings | Shop name and receipt footer, receipt printer (paper width, drawer kick), kitchen printer, staff, backups, start-up and full screen. | Managers |
| Checks | The Phase 0 tests: touch, scrolling, Arabic, database, server. | Anyone |
| Change my PIN | | Anyone |
| Lock the till | Signs out. The next person enters their PIN. | Anyone |

Voids are only for orders of the open day with no refunds. A voided order
stays in the records, marked, and doesn't count in sales.

A day stays open past midnight until it is closed, so late nights count as
one day.

## Staff

In **Settings > Staff**: **Add a person** (name on the on-screen keyboard,
then their PIN twice), **Rename**, **Change PIN**, **Make manager** /
**Make cashier**, **Turn off**. The till always keeps at least one active
manager. PINs are 4 digits, unique, and never 1234. Five wrong PINs in a row
lock the PIN pad for 30 seconds.

## Backups

The database is copied every 4 hours while the till is in use, and when the
day closes. The last 30 copies are kept. They go to
`Documents\Cafedesk backups` until you choose another folder in
**Settings > Backups**. Choose a USB drive if you can.

To restore one: close the app, copy the backup over
`%APPDATA%\Cafedesk POS\pos.db`, delete `pos.db-wal` and `pos.db-shm` beside
it, and open the app.

## The menu

The menu is still the demo menu. Phase 2's admin replaces it. It is stored in
the database now (not in the code), so the sync only has to fill its tables.

## Not in Phase 1

- Syncing to the server, and the admin (Phase 2).
- Arabic on printed receipts: printers need the receipt drawn as an image.
- Tax: the data has room for it; the switch comes later.
- Discounts, order notes, dine-in or takeaway.

## Try on the POS PC

- [ ] First sign-in with 1234 forces a new PIN.
- [ ] Ring up karak with less sugar and a biryani with extra parotta; the
      receipt prints both options.
- [ ] Kitchen printer on: a meal prints a kitchen ticket without prices; tea
      doesn't.
- [ ] Unplug the printer: the sale completes, a note offers Retry; plug it
      back in and Retry.
- [ ] A cashier's void asks for a manager's PIN.
- [ ] Refund one of two karaks; the day report shows it, and the expected
      cash drops by 1.00.
- [ ] Close the day with a wrong count: the Z report says short or over.
- [ ] A backup appears in the backup folder.
- [ ] Scroll a long receipt and the Orders list with one finger.
- [ ] The cash drawer opens on a cash sale (if a drawer is attached and the
      setting is on).
