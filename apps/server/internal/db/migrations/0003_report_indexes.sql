-- Reports find business days by date, and each day's sales and refunds.
CREATE INDEX business_days_org_date ON business_days(org_id, date);
CREATE INDEX refunds_day ON refunds(business_day_id);
