# POS design

## Brief

- **Subject:** the order screen of a karak and meals cafeteria in Dubai, an
  industrial district of warehouses, workshops and garages. Customers are mostly
  drivers, mechanics and workers on a short break. Karak costs a dirham or two
  and the queue moves fast.
- **Audience:** the cashier. They stand at a touch-only POS screen (around 15",
  landscape, 1024×768 to 1366×768), in a bright and sometimes hot shop, often
  with wet or greasy fingers. They may read English, Arabic or neither well.
- **Primary job:** ring up an order in a few taps, take payment and print the
  receipt (and a kitchen ticket later), without the screen ever making them wait.

## Tokens

### Color

| Name | Hex | Role |
|---|---|---|
| Concrete | `#CDD2C9` | The app's ground: the cool grey-green of warehouse floors. Quiet, low glare. |
| Tile | `#F4F5F1` | Item tiles and buttons: a matte panel on the concrete. |
| Paper | `#FFFFFF` | The receipt strip only: real thermal-paper white. |
| Ink | `#213A31` | All text: a deep pine ink, clearly green, not a stand-in for black. |
| Saffron | `#F2A20C` | **Money moves only:** Charge and Complete sale. Saffron is in the karak and in the safety yellow of the district. |
| Chili | `#C23B2E` | Destructive actions only: remove a line, clear an order. |

Category signage colors are used as an 8 DIP band at the top of each tile, never
as a fill, so text contrast never changes:

| Category | Name | Hex |
|---|---|---|
| Tea and karak | Cardamom | `#5F8A3A` |
| Meals | Harbour | `#2F6690` |
| Snacks and bread | Plum | `#7B4B94` |
| Cold drinks | Teal | `#1E8A83` |

Muted ink for secondary text is `#56675F` (4.6:1 on Tile).

### Type

One family: **Readex Pro** (SIL OFL), which covers both Latin and Arabic with
matching color and wide, clear numerals. It was designed for legibility, which
suits a screen read at arm's length.

Scale (from the classic typographic scale): 14 · 16 · 18 · 21 · 24 · 36 · 48.

| Use | Size | Weight |
|---|---|---|
| Meta (order number, time, page count) | 14 | Regular |
| Body, receipt lines | 16 | Regular |
| Tile item name | 18 | Medium |
| Prices on tiles, buttons | 21 | Medium |
| Numpad keys | 24 | Medium |
| Order total | 36 | Bold |
| Change due | 48 | Bold |

Every amount uses tabular figures (`tnum`) so columns of prices line up.

### Layout

Two zones: a **menu wall** on the left and the **receipt strip** on the right.

```
┌──────────────────────────────────────────────┬────────────────────┐
│ Tea and karak   Meals   Snacks   Drinks  ⚙ 12:41│ Order T1-012       │
├──────────────────────────────────────────────┤ ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄ │
│ ▀▀▀▀▀▀▀▀ ▀▀▀▀▀▀▀▀ ▀▀▀▀▀▀▀▀ ▀▀▀▀▀▀▀▀           │ 2  Karak chai  2.00│
│ Karak    Saffron  Ginger   Masala             │ 1  Chicken     14.00│
│ chai     karak    karak    chai               │    biryani         │
│ شاي كرك  كرك زعفران ...                        │                    │
│ 1.00     2.00     1.50     1.50               │                    │
│ (4 × 3 tiles per page)                        │ ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄ │
│                                               │ Total    AED 16.00 │
│ [ Previous ]      Page 1 of 2      [ Next ]   │ [ Charge AED 16.00 ]│
└──────────────────────────────────────────────┴─◠◠◠◠◠◠◠◠◠◠◠◠◠◠◠◠◠◠─┘
```

- Text is left-aligned; amounts are right-aligned in the receipt and sit at the
  bottom left of each tile.
- The menu wall never scrolls: 12 tiles a page, with Previous and Next. This also
  sidesteps MyGo's Windows one-finger scroll bug.
- Touch targets: tiles at least 120×120 DIPs, numpad keys 72 DIPs tall, other
  buttons at least 56 DIPs tall.
- Payment opens as a sheet over the menu wall, with a numpad, quick cash
  amounts and the change due. The receipt stays visible beside it.

### Principles

1. **Saffron means money moves.** Only Charge and Complete sale use it, so a
   cashier's eye finds them without looking.
2. **The order panel is the receipt.** The strip on the right reads exactly as
   the printout will: the same lines, the same order, the same totals. It is the
   one bold element, drawn as a sheet of thermal paper with a perforated bottom
   edge. Everything else is quiet.
3. **Category colour is signage, not paint.** A band at the top of each tile, as
   on cafeteria lightbox menus, never a fill behind text.
4. **Touch first.** No hover-only states. A pressed tile darkens and drops 2 DIPs
   so a tap is felt. No scrolling on the main screens.
5. **Bilingual by default.** English name first, Arabic beneath it, both in the
   same family.

## Review against generic defaults

| Default it could have drifted into | Check | Change made |
|---|---|---|
| Cream background with a terracotta accent | The ground is a cool green-grey; the accent is a yellow saffron, not clay. | None needed. |
| Near-black UI with one bright accent | The UI is light, for a bright shop and screen glare. | None needed. |
| SaaS card kit: identical rounded cards, one radius, soft shadows | The first draft had shadowed, 12 DIP rounded tiles. | Tiles became flat (no shadow, 4 DIP radius) with a signage band. Only the receipt strip, the hero, gets a shadow; the Charge button gets a larger 10 DIP radius to mark its rank. |
| Tinted near-black text | The first draft used `#1B1F1D`. | Ink became `#213A31`, a deliberate pine green. |
| Monospace for data labels | A receipt invites monospace. | Kept Readex Pro with tabular figures instead; the paper look comes from the strip, not the font. |
| All-caps labels, middle-dot meta strings, arrows on buttons | — | None used: sentence case, plain button labels ("Next", "Charge AED 16.00"). |
| A generic blue POS with a dark sidebar | — | The category bar is a row of plain tabs on the concrete, not a sidebar. |

## Critique pass (from rendered screens)

The screens were rendered at 1366×768 and 1024×768 with MyGo's tester
(`TestSnapshots`) and reviewed. Changes made:

- **Category tabs** use short names (Tea, Meals, Snacks, Drinks) so the bar
  fits at 1024 wide. The selected tab is a tile with its 4 DIP signage band;
  MyGo's `Selected` state had filled it with the ink accent, hiding the band, so
  it now only tells assistive technology (`AccessSelected`).
- **Tile grid** is 4 × 3 on wide screens and 3 × 4 below 1200 DIPs, so a page
  still holds 12 items and long names no longer truncate.
- **Change** is set at 48 in the payment sheet: it's the number the cashier
  reads out. The quick-cash buttons sit in a 2 × 2 grid so they fit at 1024.
- **Buttons on tile panels** (the payment sheet, the Checks screen) are paper
  with a hairline rule. Tile-coloured buttons vanished there.
- **Empty order**: the total is greyed and the button just says "Charge".

## Phase 1 screens

Same tokens and principles. What each new screen does, and how:

| Screen | Layout | Notes |
|---|---|---|
| **Lock** | Concrete ground. Left: shop name at 36, the day's state below it ("Day open since 07:02"). Right: a tile panel with four PIN dots and a paper numpad. | A 4-digit PIN unlocks at the fourth digit, so there is no Unlock button. A wrong PIN shakes the dots once (motion that answers an action) and clears them. |
| **Start the day** | One centered tile panel: "Cash in the drawer", the amount at 36, a numpad, and "Start the day" in ink. | Not saffron: no sale takes place. |
| **Till menu** | A sheet over the menu wall, opened from the cashier's name at the top right. Paper rows, each with a label and one muted line saying what it does. | Replaces the Phase 0 Checks button. |
| **Options for a line** | A sheet over the menu wall: the item's name, then each modifier group as a row of choice buttons, with the chosen one filled with ink. Price changes show as "+1.00". "Done" in ink. | Tapping a tile adds the item with its default options at once, so a plain karak stays one tap. Options are changed from the selected receipt line. |
| **Orders** | The order screen's two zones: a list of orders on the left, and on the right **the selected order drawn as its receipt strip**, with Reprint, Refund and Void below it. | The receipt-strip principle again: you look at the receipt you would reprint. |
| **Refund** | A sheet over the list: each line with − and + for the quantity to refund, a reason, then "Refund AED 2.00" in saffron. | Saffron: money moves. |
| **Day report** | Left: the report as the paper strip it prints as. Right: "Print day report", then "Close the day": count the cash on a numpad, see the difference, close in ink. | |
| **Settings** | Plain panels, as on the Checks screen: shop, receipt printer, kitchen printer, staff, backups. | Text is typed on an on-screen keyboard sheet, since the POS has no keyboard. |
| **Manager PIN** | A small centered sheet over everything: what needs approval ("Void T1-004"), PIN dots, a numpad. | Skipped when a manager is already signed in. |

Print problems show as one chili line at the top of the receipt strip with a
Retry button. They never block a sale.

### Review against defaults

- The new screens reuse the two-zone layout, the paper strip and the numpad,
  so nothing new competes with the receipt strip as the bold element.
- No modal dialogs with OK/Cancel. Sheets cover only the zone they act on, and
  the receipt stays in view.
- Saffron stays reserved for money: Charge, Complete sale, Refund. Starting
  and closing the day, voids and settings use ink, chili or plain buttons.
- Button labels say what happens: "Void T1-004", "Refund AED 2.00",
  "Close the day". None is a bare "OK" or "Confirm".

## Admin (Phase 2)

**Brief.** The admin is a web page for the owner or a manager, on a laptop or a
phone. Its first job is the menu: categories, items, prices and options.
Terminals, staff and reports come next.

**Tokens.** The same palette and Readex Pro (served by the admin itself, not a
font CDN), and the same type scale. Body text is 16. Web additions:

| Name | Hex | Role |
|---|---|---|
| Field | `#FFFFFF` with a `#AEB7AA` hairline | Text inputs: paper, so typed text sits on what it becomes |
| Focus | Ink, 3 px ring, 2 px out | Keyboard focus on every control |

**Layout.** The menu page is the till's own menu wall, at the same proportions:
category tabs, then tiles with their signage band, English and Arabic names,
and price. **The tile is the preview.** Clicking one opens its editor in a tile
panel on the right, and the tile updates as you type. The till's receipt strip
becomes the editing panel, in the same place.

```
┌ Cafedesk  Downtown ──────────── Menu   Options ──────── Owner  Sign out ┐
│ ■ Tea  ■ Meals  ■ Snacks  ■ Drinks   + Category                          │
├────────────────────────────────────────────────┬────────────────────────┤
│ ▀▀▀▀▀▀ ▀▀▀▀▀▀ ▀▀▀▀▀▀ ▀▀▀▀▀▀                     │ Edit item              │
│ Karak  Saffron Ginger Masala                    │ Name  [Karak chai    ] │
│ شاي كرك ...                                     │ Arabic [شاي كرك     ] │
│ 1.00   2.00   1.50   1.50                       │ Price [1.00]           │
│ [+ Item]                                        │ Options ☑ Sugar        │
│                                                 │ ☐ Kitchen ticket       │
│                                                 │ [Save item] [Delete]   │
└─────────────────────────────────────────────────┴────────────────────────┘
```

- Under 900 px wide: tiles in two columns, tabs scroll sideways, and the editor
  opens as a full-screen sheet.
- Left-aligned text. Labels sit above fields, in sentence case.
- Reordering is with "Move earlier" and "Move later" buttons, not dragging, so
  it works with a keyboard and a thumb.

**Principles.** The till's principles carry over: what the admin shows is what
the till shows; no saffron (no money moves in the admin); chili only for
deleting; every button says what it does ("Save item", "Delete category").

**Review against defaults.** The default admin would be a left sidebar, white
cards with shadows, a blue primary button and a data table of items. Instead:
navigation is two words in the top bar, the ground is concrete, and the menu is
shown as tiles, not a table. Tables come later for reports, where they belong.

**Critique pass.** Screenshots at 1366, 1024 and 390 wide (a phone), taken in
headless Edge, led to these changes: the top bar's links had kept the
browser's blue and purple; the editor's preview tile on a phone vanished into
the sheet's tile color, so it is paper there; and the signed-in name is hidden
on phones to keep the top bar on one line with Sign out.

### Staff, Tills and pairing

- **Admin Staff and Tills** follow the Options page: a list on the left, the
  editor on the right. A pairing code is set at 44 in a paper panel, split
  4 and 4 to read aloud, with the minutes it has left.
- **The till's pairing sheet** reuses the PIN pad, with eight dots.
- **Sync on the till** is quiet when all is well: nothing shows. "Offline",
  "Unpaired" or "Sync stopped" shows by the clock in chili only when something
  needs a look, and the till menu and Settings give the sentence.

### Sales

**Who and why.** The owner, in the evening or at the end of the week, asks
two things first: how much did we take, and does the cash add up? Then:
what sold, when it was busy, and on which till and by whom.

**The memorable thing: the slip.** The editor's place on the right, where
the till keeps its receipt strip, holds the report's totals as the till
prints its day report: paper, label left and amount right, rules between
groups. Net sales leads at 48, in the same Readex Pro, with proportional
figures. It replaces a row of stat cards, which would be the generic answer.

```
┌ Menu Options Staff Tills Sales ─────────────────────────────────────────┐
│ Sales  [Today] [Yesterday] [Last 7 days] [This month] [Last month] from–to│
├─────────────────────────────────────────────┬───────────────────────────┤
│ Sales by hour            [By day] [By hour] │ ░ paper slip ░            │
│  ┃     ┃ ┃                                  │ Sat 14 Nov                │
│  ┃ ┃ ┃ ┃ ┃ ┃                                │ Net sales                 │
│ ─┸─┸─┸─┸─┸─┸─ 7  8  9 …                     │ AED 1,234.50              │
│ Show as a table                             │ 86 orders, 14.35 average  │
│                                             │ ───────────────────────── │
│ Cash counts  (each till's day: expected,    │ Sales               1,334 │
│   counted, short or over)                   │ Refunds (2)          -100 │
│ What sold    (ranked, a thin bar per row)   │ Cash / Card               │
│ Tills | Staff                               │ Voided, not counted (1)   │
└─────────────────────────────────────────────┴───────────────────────────┘
```

- **Filters**: one row above everything, presets first, then the two dates.
  The range lives in the address, so a reload or a shared link keeps it.
  Dates are the shop's business dates, in its time zone.
- **The chart**: one series, one color. Cardamom `#1E7B57`, a green kin to
  ink, passes the dataviz checks on tile and paper (ink itself reads as gray
  at chart size). Columns at most 24 wide, 4 px rounded tops, square at the
  baseline; hairline grid in `--rule`; amounts in whole dirhams on the axis.
  Each column shows its value and orders on hover and on keyboard focus, and
  the same numbers are a table one button away. One day is by hour; a range
  is by day, with by hour one tap away. While a new range loads, the old one
  stays, dimmed.
- **Short and over**: "50.00 short" in chili, with the word, never the color
  alone. Over and exact stay ink.
- **Honesty**: a till that hasn't been in touch for 15 minutes is named under
  the slip, as its later sales aren't in yet.
- Phones: the slip comes first, full width, then the rest.

## Not designed yet

The Checks screen of Phase 0 is a diagnostic page, styled with the same tokens
but kept plain. It goes away or moves into settings after Phase 0.
