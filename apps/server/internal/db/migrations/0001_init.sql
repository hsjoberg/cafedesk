-- The schema. Every table carries org_id; shop-scoped ones carry shop_id
-- too. Changes after the first deploy go into new migrations, never here.

CREATE TABLE organizations (
    id              uuid PRIMARY KEY,
    name            text NOT NULL,
    -- Goes up with every change to the menu or the till staff; each of
    -- their rows keeps the value of its last change, so a till can ask for
    -- "what changed since N".
    catalog_version bigint NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE shops (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES organizations(id),
    name           text NOT NULL,
    currency       text NOT NULL DEFAULT 'AED',
    timezone       text NOT NULL DEFAULT 'Asia/Dubai',
    tax_registered boolean NOT NULL DEFAULT false,
    trn            text,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX shops_org ON shops(org_id);

-- People who sign in to the admin, with a username (not case sensitive)
-- and a password. Till staff sign in with PINs: see staff.
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    username      text NOT NULL,
    name          text NOT NULL,
    role          text NOT NULL CHECK (role IN ('owner', 'manager')),
    password_hash text NOT NULL,
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_username ON users(lower(username));

-- A session is kept as the SHA-256 of its token: the database never holds
-- a token that would sign anyone in.
CREATE TABLE sessions (
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    user_agent   text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);

-- Tills.
CREATE TABLE terminals (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES organizations(id),
    shop_id      uuid NOT NULL REFERENCES shops(id),
    code         text NOT NULL,
    name         text NOT NULL DEFAULT '',
    token_hash   bytea UNIQUE,
    paired_at    timestamptz,
    revoked_at   timestamptz,
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, code)
);

-- A pairing code is kept as its SHA-256, usable once, for ten minutes.
CREATE TABLE pairing_codes (
    code_hash   bytea PRIMARY KEY,
    terminal_id uuid NOT NULL REFERENCES terminals(id),
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pairing_codes_terminal ON pairing_codes(terminal_id);

-- The menu. Rows are archived, not deleted, so tills learn of deletions
-- when they sync.
CREATE TABLE categories (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations(id),
    name       text NOT NULL,
    name_ar    text NOT NULL DEFAULT '',
    color      text NOT NULL,
    position   integer NOT NULL,
    kitchen    boolean NOT NULL DEFAULT false,
    archived   boolean NOT NULL DEFAULT false,
    version    bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX categories_org ON categories(org_id, version);

CREATE TABLE items (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations(id),
    category_id uuid NOT NULL REFERENCES categories(id),
    name        text NOT NULL,
    name_ar     text NOT NULL DEFAULT '',
    price_fils  bigint NOT NULL CHECK (price_fils >= 0),
    position    integer NOT NULL,
    kitchen     boolean NOT NULL DEFAULT false,
    archived    boolean NOT NULL DEFAULT false,
    version     bigint NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX items_org ON items(org_id, version);
CREATE INDEX items_category ON items(category_id);

CREATE TABLE modifier_groups (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations(id),
    name       text NOT NULL,
    name_ar    text NOT NULL DEFAULT '',
    multiple   boolean NOT NULL DEFAULT false,
    position   integer NOT NULL,
    archived   boolean NOT NULL DEFAULT false,
    version    bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX modifier_groups_org ON modifier_groups(org_id, version);

CREATE TABLE modifiers (
    id               uuid PRIMARY KEY,
    org_id           uuid NOT NULL REFERENCES organizations(id),
    group_id         uuid NOT NULL REFERENCES modifier_groups(id),
    name             text NOT NULL,
    name_ar          text NOT NULL DEFAULT '',
    price_delta_fils bigint NOT NULL DEFAULT 0,
    is_default       boolean NOT NULL DEFAULT false,
    position         integer NOT NULL,
    archived         boolean NOT NULL DEFAULT false,
    version          bigint NOT NULL,
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX modifiers_group ON modifiers(group_id);

-- An item's option groups, in order. Changing them changes the item's
-- version.
CREATE TABLE item_modifier_groups (
    item_id  uuid NOT NULL REFERENCES items(id),
    group_id uuid NOT NULL REFERENCES modifier_groups(id),
    position integer NOT NULL,
    PRIMARY KEY (item_id, group_id)
);
CREATE INDEX item_modifier_groups_group ON item_modifier_groups(group_id);

-- People who sign in to tills with a PIN. Their changes take the
-- organization's catalog version up, as the menu's do: tills pull both.
CREATE TABLE staff (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations(id),
    name            text NOT NULL,
    role            text NOT NULL CHECK (role IN ('cashier', 'manager')),
    pin_hash        text NOT NULL,
    must_change_pin boolean NOT NULL DEFAULT true,
    active          boolean NOT NULL DEFAULT true,
    version         bigint NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX staff_org ON staff(org_id, version);

CREATE TABLE staff_shops (
    staff_id uuid NOT NULL REFERENCES staff(id),
    shop_id  uuid NOT NULL REFERENCES shops(id),
    PRIMARY KEY (staff_id, shop_id)
);

-- What tills send: business days, orders, voids and refunds. Rows keep the
-- tills' own IDs, so sending them again changes nothing.
CREATE TABLE business_days (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL REFERENCES organizations(id),
    shop_id            uuid NOT NULL REFERENCES shops(id),
    terminal_id        uuid NOT NULL REFERENCES terminals(id),
    date               date NOT NULL,
    opened_at          timestamptz NOT NULL,
    opened_by          uuid NOT NULL,
    opened_by_name     text NOT NULL,
    opening_float_fils bigint NOT NULL,
    closed_at          timestamptz,
    closed_by          uuid,
    closed_by_name     text,
    counted_cash_fils  bigint,
    received_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX business_days_shop ON business_days(shop_id, opened_at);
CREATE INDEX business_days_org_date ON business_days(org_id, date);

-- Staff are kept by ID and name as the till had them: a till may have sold
-- before it was paired, with staff the server never knew.
CREATE TABLE orders (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations(id),
    shop_id         uuid NOT NULL REFERENCES shops(id),
    terminal_id     uuid NOT NULL REFERENCES terminals(id),
    business_day_id uuid NOT NULL REFERENCES business_days(id),
    number          integer NOT NULL,
    code            text NOT NULL,
    created_at      timestamptz NOT NULL,
    staff_id        uuid NOT NULL,
    staff_name      text NOT NULL,
    total_fils      bigint NOT NULL,
    payment_method  text NOT NULL CHECK (payment_method IN ('cash', 'card')),
    tendered_fils   bigint NOT NULL,
    change_fils     bigint NOT NULL,
    status          text NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'voided')),
    voided_at       timestamptz,
    voided_by       uuid,
    voided_by_name  text,
    void_reason     text,
    received_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_day_id, number)
);
CREATE INDEX orders_shop_created ON orders(shop_id, created_at);

-- Item and modifier IDs are text: a till sells from its demo menu, with IDs
-- such as "karak", until its first sync.
CREATE TABLE order_lines (
    id              uuid PRIMARY KEY,
    order_id        uuid NOT NULL REFERENCES orders(id),
    org_id          uuid NOT NULL REFERENCES organizations(id),
    position        integer NOT NULL,
    item_id         text NOT NULL,
    name            text NOT NULL,
    qty             integer NOT NULL,
    unit_price_fils bigint NOT NULL,
    line_total_fils bigint NOT NULL,
    kitchen         boolean NOT NULL DEFAULT false
);
CREATE INDEX order_lines_order ON order_lines(order_id);

CREATE TABLE order_line_modifiers (
    line_id          uuid NOT NULL REFERENCES order_lines(id),
    order_id         uuid NOT NULL REFERENCES orders(id),
    position         integer NOT NULL,
    group_id         text NOT NULL,
    group_name       text NOT NULL,
    modifier_id      text NOT NULL,
    name             text NOT NULL,
    price_delta_fils bigint NOT NULL,
    is_default       boolean NOT NULL DEFAULT false,
    PRIMARY KEY (line_id, position)
);

CREATE TABLE refunds (
    id               uuid PRIMARY KEY,
    org_id           uuid NOT NULL REFERENCES organizations(id),
    shop_id          uuid NOT NULL REFERENCES shops(id),
    terminal_id      uuid NOT NULL REFERENCES terminals(id),
    order_id         uuid NOT NULL REFERENCES orders(id),
    business_day_id  uuid NOT NULL REFERENCES business_days(id),
    created_at       timestamptz NOT NULL,
    staff_id         uuid NOT NULL,
    staff_name       text NOT NULL,
    approved_by      uuid NOT NULL,
    approved_by_name text NOT NULL,
    reason           text NOT NULL,
    method           text NOT NULL CHECK (method IN ('cash', 'card')),
    total_fils       bigint NOT NULL,
    received_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refunds_order ON refunds(order_id);
CREATE INDEX refunds_day ON refunds(business_day_id);

CREATE TABLE refund_lines (
    refund_id     uuid NOT NULL REFERENCES refunds(id),
    order_line_id uuid NOT NULL REFERENCES order_lines(id),
    qty           integer NOT NULL,
    amount_fils   bigint NOT NULL,
    PRIMARY KEY (refund_id, order_line_id)
);

-- Each backup the server takes, so it knows when the last one was, and
-- what went wrong with one that failed.
CREATE TABLE backups (
    id           uuid PRIMARY KEY,
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz,
    name         text NOT NULL,
    destinations text NOT NULL,
    bytes        bigint,
    error        text
);
CREATE INDEX backups_started ON backups(started_at);

-- What items and options cost the shop, over time. A row applies from its
-- effective_from until the next row of the same item; a NULL cost means not
-- known. An item's first cost applies from the start ('-infinity'), as its
-- earlier sales had none. Sales look their cost up by when they were made,
-- so a change never rewrites past profit.
CREATE TABLE item_costs (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES organizations(id),
    item_id        uuid NOT NULL REFERENCES items(id),
    cost_fils      bigint CHECK (cost_fils >= 0),
    effective_from timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX item_costs_item ON item_costs(item_id, effective_from);

-- An option that takes something out ("No sugar") costs less than nothing.
CREATE TABLE modifier_costs (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES organizations(id),
    modifier_id    uuid NOT NULL REFERENCES modifiers(id),
    cost_fils      bigint,
    effective_from timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX modifier_costs_modifier ON modifier_costs(modifier_id, effective_from);

-- What the shops buy: the weekly wholesale invoice and one-off purchases
-- alike. Suppliers, products and ingredients belong to the organization; a
-- purchase belongs to the shop it was for.

-- A supplier's overdue invoice is warned of unless it isn't watched (an
-- occasional supplier), or it is quiet until a date (no invoice this time,
-- or remind me later). Recording an invoice from it ends the quiet.
CREATE TABLE suppliers (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations(id),
    name        text NOT NULL,
    archived    boolean NOT NULL DEFAULT false,
    watched     boolean NOT NULL DEFAULT true,
    quiet_until date,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX suppliers_name ON suppliers(org_id, lower(name)) WHERE NOT archived;

-- What recipes use: "Evaporated milk", in grams.
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

-- What invoices name: "Rainbow evaporated milk, 48 x 170 g", with the
-- supplier's code for it (1002683). A product is one ingredient, and holds
-- so much of it: that one holds 8160 g. Pack sizes are in thousandths of
-- the unit, as quantities are. Changing brand adds a product, not an
-- ingredient.
-- Its area is where its cost goes. Sales count to the kitchen for items on
-- the kitchen ticket, and to the counter for the rest. counter: tea and
-- drinks; kitchen: meals; packaging: cups, boxes, bags.
CREATE TABLE products (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    name          text NOT NULL,
    code          text,
    area          text NOT NULL CHECK (area IN ('counter', 'kitchen', 'packaging', 'other')),
    ingredient_id uuid REFERENCES ingredients(id),
    pack_milli    bigint CHECK (pack_milli > 0),
    archived      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX products_name ON products(org_id, lower(name)) WHERE NOT archived;
CREATE INDEX products_code ON products(org_id, code) WHERE code IS NOT NULL;
CREATE INDEX products_ingredient ON products(ingredient_id);

-- A deleted purchase is kept, with who deleted it, for the record. Its
-- total is what was paid, VAT included; vat_fils is the VAT in it, kept
-- apart in case the shop registers for VAT, when it is reclaimable.
CREATE TABLE purchases (
    id                uuid PRIMARY KEY,
    org_id            uuid NOT NULL REFERENCES organizations(id),
    shop_id           uuid NOT NULL REFERENCES shops(id),
    date              date NOT NULL,
    supplier_id       uuid REFERENCES suppliers(id),
    reference         text NOT NULL DEFAULT '',
    prices_before_vat boolean NOT NULL DEFAULT false,
    total_fils        bigint NOT NULL,
    vat_fils          bigint NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL,
    created_by        uuid NOT NULL REFERENCES users(id),
    updated_at        timestamptz,
    updated_by        uuid REFERENCES users(id),
    deleted_at        timestamptz,
    deleted_by        uuid REFERENCES users(id)
);
CREATE INDEX purchases_org_date ON purchases(org_id, date);

-- Quantities are in thousandths, so 1.5 kg is 1500. A line keeps its area,
-- as the product's may change later. Its total includes its VAT.
CREATE TABLE purchase_lines (
    purchase_id     uuid NOT NULL REFERENCES purchases(id),
    position        integer NOT NULL,
    product_id      uuid NOT NULL REFERENCES products(id),
    area            text NOT NULL CHECK (area IN ('counter', 'kitchen', 'packaging', 'other')),
    qty_milli       bigint NOT NULL CHECK (qty_milli > 0),
    unit_price_fils bigint NOT NULL CHECK (unit_price_fils >= 0),
    total_fils      bigint NOT NULL CHECK (total_fils >= 0),
    vat_fils        bigint NOT NULL DEFAULT 0 CHECK (vat_fils >= 0),
    PRIMARY KEY (purchase_id, position)
);
CREATE INDEX purchase_lines_product ON purchase_lines(product_id);

-- What goes into what the shop sells. A batch is something made in bulk:
-- its ingredients, and how many portions it makes. A recipe line belongs to
-- an item, an option or a batch, and is an amount of an ingredient (in its
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
