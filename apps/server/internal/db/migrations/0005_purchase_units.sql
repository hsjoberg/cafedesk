-- The unit an invoice line counts in, as the invoice prints it: EA, CAR,
-- PAC, BAG... Optional: '' for a line that doesn't say.
ALTER TABLE purchase_lines ADD COLUMN uom text NOT NULL DEFAULT '';

-- A product's pack_milli is how much one of pack_uom holds ('' for a pack
-- size given without a unit, which goes for any line). The other units it
-- comes in each hold their own amount: one EA of mozzarella 2,000 g, one
-- CAR of it 12,000 g.
ALTER TABLE products ADD COLUMN pack_uom text NOT NULL DEFAULT '';

CREATE TABLE product_packs (
    product_id uuid NOT NULL REFERENCES products(id),
    uom        text NOT NULL CHECK (uom <> ''),
    pack_milli bigint NOT NULL CHECK (pack_milli > 0),
    PRIMARY KEY (product_id, uom)
);
