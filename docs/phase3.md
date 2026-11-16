# Phase 3: costs and margins

Phase 3 lets the admin show what the shop makes, not only what it takes.

| Step | What | State |
|---|---|---|
| 1 | A cost for each item and option, and profit on the Sales page | Done |
| 2 | Ingredients, purchases and recipes: an item's cost from what goes into it | Next |

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

`costFils` on `ItemInput`/`Item` and `ModifierInput`/`Modifier`: required, and
`null` for not known, so saving an item always says what its cost is. The
sales report adds `costFils`, `profitFils` and `uncostedFils` to the totals,
and `costFils` to each item (missing when some of its sales had no cost).

The history is in `item_costs` and `modifier_costs` (migration 0005). The
report looks a sale's cost up by its time, so no cost is copied onto sales;
step 2 can still do that, when costs come from ingredient prices.

The `demo` command gives the demo menu made-up costs, leaving every fifth
item without one.
