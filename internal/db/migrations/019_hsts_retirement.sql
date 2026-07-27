-- HSTS remains cached in visitors' browsers after the header is disabled.
-- Keep the certificate attached until the previously advertised max-age has
-- elapsed, otherwise returning visitors can be locked onto a TLS endpoint
-- that no longer has a certificate.
ALTER TABLE sites ADD COLUMN hsts_retire_after TEXT;
