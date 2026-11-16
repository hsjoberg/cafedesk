# Phase 3: costs and margins

Phase 3 lets the admin show what the shop makes, not only what it takes.

| Step | What | State |
|---|---|---|
| 1 | A cost for each item and option, and profit on the Sales page | Done |
| 2.1 | Purchases: what was bought, and where the money went | Done |
| 2.2 | Products and ingredients: pack sizes, and a price per gram or millilitre | Next |
| 2.3 | Recipes and the chef's batch: an item's cost from what goes into it | |
| 2.4 | The gap: what was bought against what sales should have used | |

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

**Where the money went**, for the dates chosen (this month at first): what
each area bought, against its sales. The kitchen's sales are the items on the
kitchen ticket; the counter's, the rest; packaging and other are against all
sales. Sales are less refunds, by business day, as on the Sales page. Stock
bought one week is used the next, so a month says more than a week.

The `demo` command adds a wholesale invoice every Monday and a few one-off
buys every few days.

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
the supplier likewise. Quantities have up to 3 decimals, kept as thousandths
(migration 0006).

`costFils` on `ItemInput`/`Item` and `ModifierInput`/`Modifier`: required, and
`null` for not known, so saving an item always says what its cost is. The
sales report adds `costFils`, `profitFils` and `uncostedFils` to the totals,
and `costFils` to each item (missing when some of its sales had no cost).

The history is in `item_costs` and `modifier_costs` (migration 0005). The
report looks a sale's cost up by its time, so no cost is copied onto sales;
step 2 can still do that, when costs come from ingredient prices.

The `demo` command gives the demo menu made-up costs, leaving every fifth
item without one.
