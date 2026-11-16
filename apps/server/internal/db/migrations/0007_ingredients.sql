-- What recipes use: "Evaporated milk", in grams. A product (what an invoice
-- names) is one ingredient, and holds so much of it: "Evaporated milk, 48 x
-- 170 g" holds 8160 g. Pack sizes are in thousandths of the unit, as
-- quantities are. Changing brand adds a product, not an ingredient.
CREATE TABLE ingredients (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations(id),
    name       text NOT NULL,
    unit       text NOT NULL CHECK (unit IN ('g', 'ml', 'piece')),
    archived   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ingredients_name ON ingredients(org_id, lower(name)) WHERE NOT archived;

ALTER TABLE products
    ADD COLUMN ingredient_id uuid REFERENCES ingredients(id),
    ADD COLUMN pack_milli bigint CHECK (pack_milli > 0);
CREATE INDEX products_ingredient ON products(ingredient_id);
