-- Keep the certbot lineage and ACME provider as durable certificate identity.
-- The X.509 issuer is display metadata (for example "R13"), not the provider
-- account identity and not necessarily the certbot --cert-name.
ALTER TABLE ssl_certificates ADD COLUMN lineage_name TEXT;
ALTER TABLE ssl_certificates ADD COLUMN acme_provider_id TEXT;

-- Certificates created before this migration used the canonical domain name
-- as their certbot lineage. Custom certificates deliberately keep both
-- columns NULL.
UPDATE ssl_certificates
SET lineage_name = (
        SELECT lower(trim(d.name))
        FROM domains d
        WHERE d.id = ssl_certificates.domain_id
    ),
    acme_provider_id = CASE lower(trim(COALESCE(issuer, '')))
        WHEN 'let''s encrypt' THEN 'letsencrypt'
        WHEN 'zerossl' THEN 'zerossl'
        WHEN 'google trust services' THEN 'google'
        ELSE NULL
    END
WHERE type = 'letsencrypt';

CREATE INDEX idx_ssl_certificates_lineage
    ON ssl_certificates(lineage_name)
    WHERE lineage_name IS NOT NULL;
