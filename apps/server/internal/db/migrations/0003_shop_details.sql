-- What a shop's receipts print at the top. The UAE's consumer protection
-- law asks for the trade name and the address, in Arabic (other languages
-- may stand beside it); the phone and the licence number are for the
-- customer's sake. Empty until filled in, in the admin.
ALTER TABLE shops
    ADD COLUMN trade_name     text NOT NULL DEFAULT '',
    ADD COLUMN trade_name_ar  text NOT NULL DEFAULT '',
    ADD COLUMN address        text NOT NULL DEFAULT '',
    ADD COLUMN address_ar     text NOT NULL DEFAULT '',
    ADD COLUMN phone          text NOT NULL DEFAULT '',
    ADD COLUMN licence_number text NOT NULL DEFAULT '';
