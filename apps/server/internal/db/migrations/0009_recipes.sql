-- What goes into what the shop sells. A batch is the chef's big cook: its
-- ingredients, and how many portions it makes. A recipe line belongs to an
-- item, an option or a batch, and is an amount of an ingredient (in its
-- unit) or, for items and options, portions of a batch. An option's lines
-- adjust its item's: "No sugar" is -10 g of sugar. Amounts are in
-- thousandths, as quantities are.

CREATE TABLE batches (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations(id),
    name        text NOT NULL,
    yield_milli bigint NOT NULL CHECK (yield_milli > 0),
    archived    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX batches_name ON batches(org_id, lower(name)) WHERE NOT archived;

CREATE TABLE recipe_lines (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    item_id       uuid REFERENCES items(id),
    modifier_id   uuid REFERENCES modifiers(id),
    batch_id      uuid REFERENCES batches(id),
    position      integer NOT NULL,
    ingredient_id uuid REFERENCES ingredients(id),
    part_batch_id uuid REFERENCES batches(id),
    amount_milli  bigint NOT NULL CHECK (amount_milli <> 0),
    CHECK (num_nonnulls(item_id, modifier_id, batch_id) = 1),
    CHECK (num_nonnulls(ingredient_id, part_batch_id) = 1),
    CHECK (batch_id IS NULL OR part_batch_id IS NULL)
);
CREATE INDEX recipe_lines_item ON recipe_lines(item_id);
CREATE INDEX recipe_lines_modifier ON recipe_lines(modifier_id);
CREATE INDEX recipe_lines_batch ON recipe_lines(batch_id);
CREATE INDEX recipe_lines_part ON recipe_lines(part_batch_id);

-- An option that takes something out ("No sugar") costs less than nothing.
ALTER TABLE modifier_costs DROP CONSTRAINT modifier_costs_cost_fils_check;
