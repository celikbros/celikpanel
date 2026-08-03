-- A site's filesystem location is tenant identity, not a customer setting.
-- Hosted sites have one canonical root derived from subscription/domain IDs.
-- DNS-only rows deliberately have no filesystem and therefore use the empty
-- string (sites.document_root predates DNS-only and is NOT NULL).
--
-- Do not silently bless legacy rows. Moving a live tree requires a separate,
-- explicit filesystem migration, so startup fails closed when an old row does
-- not already match one of the two valid representations.
CREATE TEMP TABLE site_layout_identity_preflight (
    ok INTEGER NOT NULL CHECK (ok = 1)
);

INSERT INTO site_layout_identity_preflight (ok)
SELECT CASE
    WHEN EXISTS (
        SELECT 1
        FROM sites AS s
        LEFT JOIN domains AS d ON d.id = s.domain_id
        WHERE d.id IS NULL
           OR (
                s.project_type = 'dnsonly'
                AND s.document_root <> ''
           )
           OR (
                s.project_type <> 'dnsonly'
                AND s.document_root <> (
                    '/var/www/celikpanel/subscriptions/' || d.subscription_id ||
                    '/sites/' || s.domain_id || '/public_html'
                )
           )
    )
    THEN 0
    ELSE 1
END;

DROP TABLE site_layout_identity_preflight;

CREATE TRIGGER trg_sites_document_root_identity_insert
BEFORE INSERT ON sites
WHEN NOT (
    (
        NEW.project_type = 'dnsonly'
        AND NEW.document_root = ''
        AND EXISTS (SELECT 1 FROM domains WHERE id = NEW.domain_id)
    )
    OR
    (
        NEW.project_type <> 'dnsonly'
        AND NEW.document_root = (
            SELECT '/var/www/celikpanel/subscriptions/' || subscription_id ||
                   '/sites/' || NEW.domain_id || '/public_html'
            FROM domains
            WHERE id = NEW.domain_id
        )
    )
)
BEGIN
    SELECT RAISE(
        ABORT,
        'site document_root must match its hosting role and identity-derived subscription/domain path'
    );
END;

-- Hosted project types may change (for example php -> static), but DNS-only
-- is an OS-resource boundary. Converting across that boundary needs an
-- orchestrated create/delete workflow, not a row-only UPDATE.
CREATE TRIGGER trg_sites_hosting_role_immutable
BEFORE UPDATE OF project_type ON sites
WHEN (OLD.project_type = 'dnsonly') <> (NEW.project_type = 'dnsonly')
BEGIN
    SELECT RAISE(
        ABORT,
        'site DNS-only hosting role cannot be changed without an orchestrated filesystem transition'
    );
END;

CREATE TRIGGER trg_sites_document_root_identity_update
BEFORE UPDATE OF document_root, project_type ON sites
WHEN NOT (
    (
        NEW.project_type = 'dnsonly'
        AND NEW.document_root = ''
        AND EXISTS (SELECT 1 FROM domains WHERE id = NEW.domain_id)
    )
    OR
    (
        NEW.project_type <> 'dnsonly'
        AND NEW.document_root = (
            SELECT '/var/www/celikpanel/subscriptions/' || subscription_id ||
                   '/sites/' || NEW.domain_id || '/public_html'
            FROM domains
            WHERE id = NEW.domain_id
        )
    )
)
BEGIN
    SELECT RAISE(
        ABORT,
        'site document_root must match its hosting role and identity-derived subscription/domain path'
    );
END;

-- domain_id is part of the path identity. Re-parenting a site row without
-- moving and re-validating its tree would make the database lie about disk.
CREATE TRIGGER trg_sites_domain_identity_immutable
BEFORE UPDATE OF domain_id ON sites
WHEN NEW.domain_id <> OLD.domain_id
BEGIN
    SELECT RAISE(
        ABORT,
        'site domain_id is immutable; use an orchestrated site migration'
    );
END;

-- A DNS-only domain owns no tree, so its subscription may be reassigned.
-- Once it has a hosted site, changing subscription_id also changes the
-- canonical path and is therefore blocked until a real filesystem migration
-- exists.
CREATE TRIGGER trg_domains_hosted_subscription_identity_immutable
BEFORE UPDATE OF subscription_id ON domains
WHEN NEW.subscription_id <> OLD.subscription_id
 AND EXISTS (
    SELECT 1
    FROM sites
    WHERE domain_id = OLD.id
      AND project_type <> 'dnsonly'
 )
BEGIN
    SELECT RAISE(
        ABORT,
        'hosted domain subscription_id is immutable; use an orchestrated site migration'
    );
END;
