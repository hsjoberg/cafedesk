-- Cash put into a till's drawer or taken out of it, other than by a sale or
-- a refund: takings banked, a supplier paid from the drawer, change added.
-- Above 0 is put in, below 0 taken out. Like the rest of what tills send,
-- rows keep the till's own IDs.
CREATE TABLE cash_movements (
    id               uuid PRIMARY KEY,
    org_id           uuid NOT NULL REFERENCES organizations(id),
    shop_id          uuid NOT NULL REFERENCES shops(id),
    terminal_id      uuid NOT NULL REFERENCES terminals(id),
    business_day_id  uuid NOT NULL REFERENCES business_days(id),
    created_at       timestamptz NOT NULL,
    staff_id         uuid NOT NULL,
    staff_name       text NOT NULL,
    approved_by      uuid NOT NULL,
    approved_by_name text NOT NULL,
    amount_fils      bigint NOT NULL CHECK (amount_fils <> 0),
    reason           text NOT NULL,
    received_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX cash_movements_day ON cash_movements(business_day_id);
