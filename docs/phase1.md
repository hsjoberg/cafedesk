# Phase 1: the till

What the POS does now, and how to try it. Everything works offline; nothing
syncs yet (Phase 2).

## First run

From Phase 2 on, a new till first connects to its admin and gets its staff from
there; see [phase2.md](phase2.md). What follows is the till's own behavior, and
demo mode (`CAFEDESK_DEMO=1`).

1. Open **Cafedesk POS**. If a Phase 0 database is there, it is renamed to
   `pos-phase0-<date>.db` in `%APPDATA%\Cafedesk POS` and a fresh one starts.
2. The till opens locked. Sign in with **1234**: that's the **Owner**, a
   manager. The till then makes you choose your own PIN, typed twice.
3. **The till's first day**: count the cash in the drawer, type it, tap
   **Start the day**. This happens once. After that, a day starts with its
   first sale, from what the day before left in the drawer.

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
| Day report | Sales so far. **Print day report** prints an X report. **Cash in or out**: cash put in or taken out other than by a sale (banked, a supplier paid, change added), with a reason. **Close the day**, with a count of the cash or without: a count shows short or over since the last count. The Z report prints and a backup is made. | Cash in or out and closing need a manager |
| Settings | Shop name and receipt footer, receipt printer (paper width, drawer kick, receipts in English and Arabic), kitchen printer, staff, backups, the end of the day, start-up and full screen. | Managers |
| Checks | The Phase 0 tests: touch, scrolling, Arabic, database, server. | Anyone |
| Change my PIN | | Anyone |
| Lock the till | Signs out. The next person enters their PIN. | Anyone |

Voids are only for orders of the open day with no refunds. A voided order
stays in the records, marked, and doesn't count in sales.

A day stays open past midnight until it is closed, so late nights count as
one day.

### Receipts in English and Arabic

Receipts and refund slips print in English and Arabic: the business's trade
name, address, phone and licence number from the admin's **Shop** page, each
item with its Arabic name from the menu, and the labels in both. The till
draws them as a picture, as it draws its screen, so Arabic prints joined and
right to left on any receipt printer, in either print mode. Kitchen tickets
and day reports, for staff, stay as text. **Receipts in English and Arabic**
(Settings, receipt printer) turns this off, for a printer that can't print
pictures: receipts are then text, in English only, which the law doesn't
allow for customers.

### The drawer from day to day

The cash in the drawer carries over: each day starts with what the day
before left, counted or, if it wasn't, what it should have held. So the
cash needn't be counted every day. Count it when you like, say on Sundays,
by typing the count when closing the day; it is checked against every day
since the last count. Days that aren't closed by hand close by themselves.

- **End of the day** (Settings): a day still open closes by itself at this
  time, 04:00 unless changed, without a count, and the till locks. Choose a
  time after the shop closes and before it opens, or **Never**. If the till
  is off then, the day closes when it next starts, dated at that time.
- The next sale starts the next day. A day without sales is never made.
- **Cash in or out** keeps a count right when cash leaves the drawer (or
  goes in) other than by a sale or a refund.

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
- [ ] A receipt prints in English and Arabic, with the shop's details from
      the admin, in both print modes, and cuts.
- [ ] Close the day without counting: the Z report says not counted, and the
      next sale starts a day with what the drawer should hold.
- [ ] Take out cash as Banked: the day report shows it, and the expected cash
      drops by it.
- [ ] Leave a day open past the end of the day: it closes by itself and the
      till locks.
- [ ] A backup appears in the backup folder.
- [ ] Scroll a long receipt and the Orders list with one finger.
- [ ] The cash drawer opens on a cash sale (if a drawer is attached and the
      setting is on).
