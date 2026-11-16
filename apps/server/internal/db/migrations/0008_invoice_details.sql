-- What real wholesale invoices have: prices before VAT with VAT added per
-- line, and the wholesaler's item codes. And a say over the warning that a
-- supplier's invoice is overdue.

-- A line's total is what was paid, VAT included; vat_fils is the VAT in it,
-- kept apart in case the shop registers for VAT, when it is reclaimable.
ALTER TABLE purchases
    ADD COLUMN prices_before_vat boolean NOT NULL DEFAULT false,
    ADD COLUMN vat_fils bigint NOT NULL DEFAULT 0;
ALTER TABLE purchase_lines
    ADD COLUMN vat_fils bigint NOT NULL DEFAULT 0 CHECK (vat_fils >= 0);

-- The supplier's code for a product, as on its invoices: 1002683.
ALTER TABLE products ADD COLUMN code text;
CREATE INDEX products_code ON products(org_id, code) WHERE code IS NOT NULL;

-- A supplier's overdue invoice is warned of unless it isn't watched (an
-- occasional supplier), or it is quiet until a date (no invoice this time,
-- or remind me later). Recording an invoice from it ends the quiet.
ALTER TABLE suppliers
    ADD COLUMN watched boolean NOT NULL DEFAULT true,
    ADD COLUMN quiet_until date;
