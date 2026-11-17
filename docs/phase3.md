# Phase 3: costs and margins

Phase 3 lets the admin show what the shop makes, not only what it takes.

| Step | What | State |
|---|---|---|
| 1 | A cost for each item and option, and profit on the Sales page | Done |
| 2.1 | Purchases: what was bought, and where the money went | Done |
| 2.2 | Products and ingredients: pack sizes, and a price per kg, litre or piece | Done |
| 2.3 | Recipes and the chef's batch: an item's cost from what goes into it | Done |
| 2.4 | Bought against used: what was bought against what sales used, by the recipes | Done |

## Purchases

**Purchases**, in the admin, records what the shop buys: the weekly wholesale
invoice and one-off buys alike.

- A purchase has its date, its supplier (empty for a one-off from anywhere),
  the invoice number if there is one, and lines: the product as the invoice
  names it, how many, the price each and the amount. The amount is quantity
  times price until it is typed, as an invoice's rounding may differ.
- Each line says where it goes: **Counter** (tea and drinks), **Kitchen**,
  **Packaging** (cups, boxes, bags) or **Other**.
- Suppliers and products are remembered as they are typed. A known product
  fills in its area and last price. For a known supplier, **Copy the lines of
  the last invoice** starts from its previous invoice, prices kept and
  quantities empty: the week's work is the quantities.
- **The invoice's own total**, typed in, is checked against the lines before
  saving: "The lines come to 2.50 less than the invoice".
- Purchases can be changed and deleted. Who entered and changed each is kept,
  and a deleted one stays in the database, marked, for the record.
- **A new product is sorted on its line.** Under it: "New product. Which
  ingredient is it?", filled in from its name: "Basmati rice, 10 kg" suggests
  Basmati rice, 10,000 g, and the area of the other rice. "A new
  ingredient…" names one from the product ("Ghee, 4 x 1 L": Ghee, 4,000 ml).
  "None, or sort it later" leaves it for the Ingredients page. A product is
  sorted once: buying it again never changes its ingredient.
- **Prices before VAT.** A wholesale invoice gives each line's rate before
  5% VAT, and the amount with it. With **Prices are before 5% VAT** ticked,
  you type the rate and the amount includes VAT; the total says how much of it
  is VAT. As the shop isn't VAT registered, what it paid, VAT included, is its
  cost. The VAT is kept apart on each line, should it register. A supplier's
  next invoice starts with the box as its last had it. Invoices round VAT
  their own way, a fils here or there: amounts stay editable, and the
  invoice-total check shows a difference.
- **Item codes.** Each line has a box for the supplier's item code
  (1002683). A known code brings its product, area and last rate; a new
  product keeps its code, and a known one learns it the first time an invoice
  gives it. A code names its product even when the invoice's description
  changes.
- **Sizes read from names**, for the ingredient row: "Uncle J's Gold Long
  Grain Rice 35kg" is 35,000 g, "Coca Cola Zero Can 24x330ml" 7,920 ml or 24
  cans, "Spoonz Vegetable Samosa 5x25x40gm" 5,000 g or 125 samosas, "Steel
  Wool Scrubber 60x6pcs" 360 pieces. Lengths ("Cling Film 6x45cm") are no
  amount. For an ingredient counted in pieces, the count is used.
- **An invoice is recorded once.** The same invoice number from the same
  supplier is refused: "Invoice INV-1043 from Demo Wholesale is already
  recorded, on 2 Nov 2026."
- **A wholesale invoice can't go missing quietly.** The server learns how far
  apart each supplier's invoices are (the median gap of its last six). When
  none has come for that long and 2 days more, a warning heads the Sales and
  Purchases pages: "No invoice from Demo Wholesale since Mon, 2 Nov, 15 days
  ago. It usually comes every 7 days." It needs two invoices to learn the
  gap. The warning offers:
  - **Record it now**, which starts the purchase with the supplier in;
  - **No invoice this time**, for a week nothing was ordered: quiet until the
    next would be overdue;
  - **Remind me in 2 days**;
  - **Don't watch** the supplier, for one bought from now and then. A
    purchase from it offers to watch it again.

  These are kept on the server, so they hold for everyone, on any device.
  Recording an invoice from the supplier ends any quiet.

**Where the money went**, for the dates chosen (this month at first): what
each area bought, against its sales. The kitchen's sales are the items on the
kitchen ticket; the counter's, the rest; packaging and other are against all
sales. Sales are less refunds, by business day, as on the Sales page. Stock
bought one week is used the next, so a month says more than a week.

The `demo` command adds a wholesale invoice every Monday and a few one-off
buys every few days.

## Ingredients

**Ingredients**, in the admin, sorts what the shop buys into what recipes
use.

- An **ingredient** is what recipes call it, such as "Evaporated milk", in
  grams, millilitres or pieces.
- A **product** is what invoices name, such as "Evaporated milk, 48 x 170 g".
  Each is one ingredient and holds so much of it: 8,160 g. The pack size
  field takes "48 x 170" as it is on the label. Another brand or size is
  another product of the same ingredient, so no recipe changes.
- An ingredient's **price per unit** comes from the latest three purchase
  lines of its products, weighted by quantity: a big buy counts for more than
  a small one. It shows per kg, per litre or each, marked "Average of the last
  3 buys". Each product shows its own price per kg too, so pack sizes compare:
  the 20 kg bag at 4.75 per kg against the 10 kg at 6.00.
- **Not sorted yet** lists the products that are no ingredient: new ones from
  purchases, and those recipes don't use, such as dish soap, which can stay.
- Sorting a new product into a new ingredient is one save: the product's
  editor offers "A new ingredient".
- An ingredient's unit can't change while its products have pack sizes in it.
  Deleting an ingredient leaves its products, unsorted.

The `demo` command sorts its products, leaving dish soap and lemons unsorted.

## Recipes

**Recipes**, in the admin, writes down what goes into what the shop sells.

- An **item's recipe** is lines of an ingredient and an amount in its unit
  ("Evaporated milk, 30 g"), or portions of a batch ("Chicken curry base, 1
  portion"). The editor gives each line's cost at today's prices, and what
  the item costs and makes on each sale, as you type.
- A **batch** is optional: for something made in bulk, such as the chef's big
  cook, a tray of dough or a jug of syrup. It says what goes into one batch,
  and how many portions it makes; a portion costs the batch's cost over its
  portions. An item uses 1 portion, or 1.5 for a big plate. A batch is made of
  ingredients, not of other batches. What is made to order needs no batch:
  its recipe lists the ingredients for one, as served. The page puts the menu
  first and batches after it, marked optional, so a shop that cooks to order
  never needs them.
- An **option's recipe** changes its item's: "Extra sugar" +5 g of sugar,
  "No sugar" -10 g, "Extra rice" half a portion of the rice pot. It may cost
  less than nothing.
- **Costs follow by themselves.** An item or option with a recipe gets its
  cost from it, and it is worked out again whenever a recipe or batch is
  saved, a purchase is recorded, changed or deleted, or a product is sorted:
  each change goes into the cost history from that moment, so past profit
  never moves. A purchase entered late moves costs from when it was entered,
  not from its date. On Menu and Options such a cost shows as "Cost, from its
  recipe", not a field. Removing a recipe brings the typed cost back.
- **A missing price is said, not guessed.** If an ingredient has no price yet,
  the item has no cost, and the editor names the ingredient.
- An ingredient or batch a recipe uses can't be deleted until it's taken out.

## Bought against used

On **Purchases**, for the dates chosen: each ingredient's purchases against
what sales used by the recipes, the largest difference in money first.

- **Bought** is purchases of products sorted into an ingredient with a pack
  size, in its unit. **Used** is each item sold times its recipe, batches
  broken into their ingredients, options' adjustments included. Refunded
  items count as used (the food was made); voided orders don't.
- Above the table: what was bought, and what sales used, in money. When items
  without a recipe sold, it says how much, as "more bought" reads high until
  they have recipes. Purchases of unsorted products are said too.
- How to read it, on the page: more bought than used is stock on the shelf,
  waste, or a recipe asking too little; more used than bought, stock from
  before the dates, or a recipe asking too much. Without stock counts (Phase
  5) a month says more than a week.
- Used is by today's recipes: no history of recipes is kept.

The `demo` command adds three batches, recipes for the teas and some meals,
and the sugar and extras options.

## Costs

- **Menu**: an item's editor has **Cost to make or buy**, beside its price,
  and says what one makes: "0.75 profit on each, 75% of the price".
- **Options**: each option has a **Cost** too, for what it adds, as an extra
  parotta does. Empty counts as nothing, right for "Less sugar".
- Costs are for the admin only. Tills never get them.

**Changing a cost never rewrites the past.** The server keeps every cost an
item has had, with when it started. A sale counts at the cost its item had
when it was sold, by the till's clock. So:

- An item's **first** cost counts for its earlier sales too, as they had none:
  enter today's costs and last month's profit appears.
- A **change** counts from the moment it is saved: when milk goes up, the new
  cost of karak counts from then, and last week keeps the old one.
- **Emptying** a cost leaves its later sales without one.

## Profit on the Sales page

- Under the totals: **What it cost**, **Profit**, and the margin. Profit is
  the sales of items with a cost, less refunds of them, less what they cost.
- Sales of items without a cost stay out of it, and the totals say how much
  that was, so a missing cost never makes profit look better or worse.
- **What sold** has each item's profit and margin, or "No cost".
- Voided orders count for nothing, as before. A refunded item's cost still
  counts: the food was made.

## The API

Purchases: `GET/POST /v1/admin/purchases`, `PUT/DELETE /v1/admin/purchases/{id}`,
`GET /v1/admin/suppliers` and `GET /v1/admin/products`. A line names a known
product by `productId`, or a product by `productName`, which is added if new;
the supplier likewise. Quantities have up to 3 decimals, kept as thousandths.

Recipes: `GET /v1/admin/recipes`, `PUT /v1/admin/items/{id}/recipe`,
`PUT /v1/admin/modifiers/{id}/recipe`, `POST /v1/admin/batches`,
`PUT/DELETE /v1/admin/batches/{id}`. Bought against used:
`GET /v1/admin/reports/usage`.

Ingredients: `GET/POST /v1/admin/ingredients`, `PUT/DELETE
/v1/admin/ingredients/{id}`, and `PUT /v1/admin/products/{id}` for a product's
name, area, ingredient and pack size. `unitCostFils` is fils
per gram, millilitre or piece.

`costFils` on `ItemInput`/`Item` and `ModifierInput`/`Modifier`: required, and
`null` for not known, so saving an item always says what its cost is. The
sales report adds `costFils`, `profitFils` and `uncostedFils` to the totals,
and `costFils` to each item (missing when some of its sales had no cost).

The history is in `item_costs` and `modifier_costs`. The
report looks a sale's cost up by its time, so no cost is copied onto sales;
step 2 can still do that, when costs come from ingredient prices.

The `demo` command gives the demo menu made-up costs, leaving every fifth
item without one.
