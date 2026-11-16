-- Till staff, pairing tills, and what tills send: business days, orders,
-- voids and refunds. Sales rows keep the tills' own IDs, so sending them
-- again changes nothing.

ALTER TABLE terminals ADD COLUMN last_seen_at timestamptz;

-- A pairing code is kept as its SHA-256, usable once, for ten minutes.
CREATE TABLE pairing_codes (
    code_hash   bytea PRIMARY KEY,
    terminal_id uuid NOT NULL REFERENCES terminals(id),
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pairing_codes_terminal ON pairing_codes(terminal_id);

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

CREATE TABLE refund_lines (
    refund_id     uuid NOT NULL REFERENCES refunds(id),
    order_line_id uuid NOT NULL REFERENCES order_lines(id),
    qty           integer NOT NULL,
    amount_fils   bigint NOT NULL,
    PRIMARY KEY (refund_id, order_line_id)
);
