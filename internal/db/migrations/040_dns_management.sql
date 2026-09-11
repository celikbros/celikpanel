-- A setup default is not authority to move an existing domain's DNS.
-- Every pre-existing domain stays locally owned; new domains choose once.
ALTER TABLE domains ADD COLUMN dns_management TEXT NOT NULL DEFAULT 'local'
    CHECK (dns_management IN ('local', 'external', 'existing'));

CREATE TRIGGER domain_dns_management_immutable
BEFORE UPDATE OF dns_management ON domains
WHEN NEW.dns_management != OLD.dns_management
BEGIN
    SELECT RAISE(ABORT, 'DNS ownership migration requires an explicit supported workflow');
END;

CREATE TRIGGER domain_dns_management_parent
BEFORE INSERT ON domains
WHEN NEW.parent_domain_id IS NOT NULL AND NEW.dns_management !=
    (SELECT dns_management FROM domains WHERE id = NEW.parent_domain_id)
BEGIN
    SELECT RAISE(ABORT, 'A child domain must retain its parent DNS ownership');
END;

CREATE TRIGGER external_domain_no_local_zone
BEFORE INSERT ON pdns_domains
WHEN EXISTS (SELECT 1 FROM domains WHERE name = NEW.name AND dns_management != 'local')
BEGIN
    SELECT RAISE(ABORT, 'External DNS is managed at its provider');
END;

CREATE TRIGGER external_domain_existing_zone
BEFORE INSERT ON domains
WHEN NEW.dns_management != 'local' AND EXISTS
    (SELECT 1 FROM pdns_domains WHERE name = NEW.name)
BEGIN
    SELECT RAISE(ABORT, 'Existing authoritative zones retain local ownership');
END;

CREATE TRIGGER domain_dns_management_parent_update
BEFORE UPDATE OF parent_domain_id ON domains
WHEN NEW.parent_domain_id IS NOT NULL AND NEW.dns_management !=
    (SELECT dns_management FROM domains WHERE id = NEW.parent_domain_id)
BEGIN
    SELECT RAISE(ABORT, 'A child domain must retain its parent DNS ownership');
END;
