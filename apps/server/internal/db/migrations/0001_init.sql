-- Organizations, shops, admin users and their sessions, terminals, and the
-- menu. Every table carries org_id; shop-scoped ones carry shop_id too.

CREATE TABLE organizations (
    id              uuid PRIMARY KEY,
    name            text NOT NULL,
    -- Goes up with every change to the menu; each menu row keeps the value
    -- of its last change, so a terminal can ask for "what changed since N".
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

-- People who sign in to the admin. Till staff, who sign in with PINs, come
-- in a later migration.
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    email         text NOT NULL,
    name          text NOT NULL,
    role          text NOT NULL CHECK (role IN ('owner', 'manager')),
    password_hash text NOT NULL,
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email ON users(lower(email));

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

-- Terminals are paired in a later step; the table is here so shops have
-- their shape from the start.
CREATE TABLE terminals (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations(id),
    shop_id    uuid NOT NULL REFERENCES shops(id),
    code       text NOT NULL,
    name       text NOT NULL DEFAULT '',
    token_hash bytea UNIQUE,
    paired_at  timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, code)
);

-- The menu. Rows are archived, not deleted, so terminals learn of deletions
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
