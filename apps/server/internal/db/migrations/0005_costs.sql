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

CREATE TABLE modifier_costs (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES organizations(id),
    modifier_id    uuid NOT NULL REFERENCES modifiers(id),
    cost_fils      bigint CHECK (cost_fils >= 0),
    effective_from timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX modifier_costs_modifier ON modifier_costs(modifier_id, effective_from);
