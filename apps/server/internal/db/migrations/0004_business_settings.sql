-- The business's currency, country and VAT on purchases, for the whole
-- organization, so the totals of all its shops add up in one currency.
-- Businesses before this were in the UAE, in dirhams. Amounts stay in
-- *_fils columns: the currency's smallest unit, whatever it is called.
ALTER TABLE organizations
    ADD COLUMN currency    text NOT NULL DEFAULT 'AED',
    -- 'AE' follows the UAE's rules; '' none in particular.
    ADD COLUMN country     text NOT NULL DEFAULT 'AE',
    ADD COLUMN vat_percent numeric(5, 2) NOT NULL DEFAULT 5 CHECK (vat_percent >= 0 AND vat_percent <= 50);

UPDATE organizations o SET currency = sh.currency
    FROM (SELECT DISTINCT ON (org_id) org_id, currency FROM shops ORDER BY org_id, created_at) sh
    WHERE sh.org_id = o.id;

ALTER TABLE shops DROP COLUMN currency;
