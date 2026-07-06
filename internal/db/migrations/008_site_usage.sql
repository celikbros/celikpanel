-- Measured usage per site, cached in the ledger. Lists always read these
-- columns (instant); the numbers refresh when the domain detail page asks
-- for a measurement — never as a side effect of opening a list.
--
-- Site başına ölçülmüş kullanım, defterde önbellekli. Listeler her zaman bu
-- sütunları okur (anlık); sayılar domain detay sayfası ölçüm istediğinde
-- tazelenir — asla bir liste açmanın yan etkisi olarak değil.
ALTER TABLE sites ADD COLUMN disk_usage_bytes INTEGER DEFAULT 0;
ALTER TABLE sites ADD COLUMN traffic_month_bytes INTEGER DEFAULT 0;
ALTER TABLE sites ADD COLUMN usage_updated_at TEXT;
