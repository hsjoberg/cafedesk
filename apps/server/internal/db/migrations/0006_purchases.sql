-- What the shops buy: the weekly wholesale invoice and one-off purchases
-- alike. Suppliers and products belong to the organization; a purchase
-- belongs to the shop it was for. Products are what invoices name ("Rainbow
-- evaporated milk, 48 x 170 g"); later, each will belong to an ingredient.

CREATE TABLE suppliers (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations(id),
    name       text NOT NULL,
    archived   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX suppliers_name ON suppliers(org_id, lower(name)) WHERE NOT archived;

-- Where a purchase goes. Sales count to the kitchen for items on the
-- kitchen ticket, and to the counter for the rest.
-- counter: tea and drinks; kitchen: meals; packaging: cups, boxes, bags.
CREATE TABLE products (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations(id),
    name       text NOT NULL,
    area       text NOT NULL CHECK (area IN ('counter', 'kitchen', 'packaging', 'other')),
    archived   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX products_name ON products(org_id, lower(name)) WHERE NOT archived;

-- A deleted purchase is kept, with who deleted it, for the record.
CREATE TABLE purchases (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations(id),
    shop_id     uuid NOT NULL REFERENCES shops(id),
    date        date NOT NULL,
    supplier_id uuid REFERENCES suppliers(id),
    reference   text NOT NULL DEFAULT '',
    total_fils  bigint NOT NULL,
    created_at  timestamptz NOT NULL,
    created_by  uuid NOT NULL REFERENCES users(id),
    updated_at  timestamptz,
    updated_by  uuid REFERENCES users(id),
    deleted_at  timestamptz,
    deleted_by  uuid REFERENCES users(id)
);
CREATE INDEX purchases_org_date ON purchases(org_id, date);

-- Quantities are in thousandths, so 1.5 kg is 1500. A line keeps its area,
-- as the product's may change later.
CREATE TABLE purchase_lines (
    purchase_id     uuid NOT NULL REFERENCES purchases(id),
    position        integer NOT NULL,
    product_id      uuid NOT NULL REFERENCES products(id),
    area            text NOT NULL CHECK (area IN ('counter', 'kitchen', 'packaging', 'other')),
    qty_milli       bigint NOT NULL CHECK (qty_milli > 0),
    unit_price_fils bigint NOT NULL CHECK (unit_price_fils >= 0),
    total_fils      bigint NOT NULL CHECK (total_fils >= 0),
    PRIMARY KEY (purchase_id, position)
);
CREATE INDEX purchase_lines_product ON purchase_lines(product_id);
