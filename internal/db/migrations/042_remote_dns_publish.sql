CREATE TABLE remote_dns_enrollments (
    code_hash TEXT PRIMARY KEY,
    expires_at INTEGER NOT NULL,
    client_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE remote_dns_clients (
    id TEXT PRIMARY KEY,
    credential_hash TEXT NOT NULL,
    label TEXT NOT NULL,
    created_at TEXT NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN (0,1))
);
CREATE TABLE remote_dns_connections (
    id TEXT PRIMARY KEY,
    endpoint TEXT NOT NULL,
    credential TEXT NOT NULL,
    enrollment_code TEXT NOT NULL,
    enrollment_hash TEXT NOT NULL DEFAULT '',
    label TEXT NOT NULL,
    nameservers_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL CHECK(status IN ('pending','ready','revoked')),
    created_at TEXT NOT NULL
);
CREATE TABLE remote_dns_zone_ownership (
    zone_name TEXT PRIMARY KEY,
    client_id TEXT NOT NULL REFERENCES remote_dns_clients(id),
    generation INTEGER NOT NULL DEFAULT 0,
    payload_hash TEXT NOT NULL DEFAULT '',
    applied_generation INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1))
);
ALTER TABLE domains ADD COLUMN dns_remote_connection_id TEXT NOT NULL DEFAULT '';
CREATE TRIGGER domain_dns_remote_connection_immutable
BEFORE UPDATE OF dns_remote_connection_id ON domains
WHEN OLD.dns_remote_connection_id != NEW.dns_remote_connection_id
BEGIN
    SELECT RAISE(ABORT,'DNS connection migration requires an explicit supported workflow');
END;
CREATE TRIGGER domain_dns_remote_connection_insert
BEFORE INSERT ON domains
WHEN (NEW.dns_management = 'existing' AND NOT EXISTS
    (SELECT 1 FROM remote_dns_connections WHERE id=NEW.dns_remote_connection_id AND status='ready'))
    OR (NEW.dns_management != 'existing' AND NEW.dns_remote_connection_id != '')
    OR (NEW.parent_domain_id IS NOT NULL AND NEW.dns_remote_connection_id !=
       (SELECT dns_remote_connection_id FROM domains WHERE id=NEW.parent_domain_id))
BEGIN
    SELECT RAISE(ABORT,'DNS connection must be authorized and match domain ownership');
END;
CREATE TRIGGER remote_zone_no_tenant_claim
BEFORE INSERT ON domains
WHEN EXISTS (SELECT 1 FROM remote_dns_zone_ownership WHERE zone_name=NEW.name)
BEGIN
    SELECT RAISE(ABORT,'hostname namespace conflict: authorized DNS publishing client');
END;
CREATE TABLE remote_dns_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    name TEXT NOT NULL, type TEXT NOT NULL, content TEXT NOT NULL,
    ttl INTEGER NOT NULL, prio INTEGER NOT NULL DEFAULT 0,
    disabled INTEGER NOT NULL DEFAULT 0 CHECK(disabled IN (0,1))
);
CREATE INDEX remote_dns_records_domain ON remote_dns_records(domain_id);
CREATE TABLE remote_dns_zones (
    domain_id INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    applied_generation INTEGER NOT NULL DEFAULT 0,
    payload_hash TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1))
);

CREATE UNIQUE INDEX remote_dns_pending_enrollment ON remote_dns_connections(endpoint,enrollment_code) WHERE status='pending';
CREATE UNIQUE INDEX remote_dns_origin_enrollment_identity ON remote_dns_connections(endpoint,enrollment_hash) WHERE enrollment_hash!='';
CREATE TRIGGER domain_dns_remote_connection_parent_update
BEFORE UPDATE OF parent_domain_id ON domains
WHEN NEW.parent_domain_id IS NOT NULL AND NEW.dns_remote_connection_id !=
    (SELECT dns_remote_connection_id FROM domains WHERE id=NEW.parent_domain_id)
BEGIN
    SELECT RAISE(ABORT,'A child domain must retain its parent DNS connection');
END;
CREATE TRIGGER remote_dns_connection_endpoint_immutable
BEFORE UPDATE OF endpoint,id ON remote_dns_connections
WHEN OLD.endpoint != NEW.endpoint OR OLD.id != NEW.id
BEGIN
    SELECT RAISE(ABORT,'DNS connection endpoint and identity are immutable');
END;
CREATE TRIGGER remote_dns_receiver_owner_immutable
BEFORE UPDATE OF client_id,zone_name ON remote_dns_zone_ownership
WHEN OLD.client_id != NEW.client_id OR OLD.zone_name != NEW.zone_name
BEGIN
    SELECT RAISE(ABORT,'Remote DNS zone ownership is immutable');
END;

CREATE TRIGGER hostname_reservations_remote_insert
BEFORE INSERT ON hostname_reservations
WHEN EXISTS(SELECT 1 FROM remote_dns_zone_ownership WHERE (NEW.hostname = zone_name OR substr(NEW.hostname,-(length(zone_name)+1))='.'||zone_name OR substr(zone_name,-(length(NEW.hostname)+1))='.'||NEW.hostname))
BEGIN SELECT RAISE(ABORT,'hostname namespace conflict: authorized DNS publishing client'); END;
CREATE TRIGGER hostname_reservations_remote_update
BEFORE UPDATE OF hostname ON hostname_reservations
WHEN EXISTS(SELECT 1 FROM remote_dns_zone_ownership WHERE (NEW.hostname = zone_name OR substr(NEW.hostname,-(length(zone_name)+1))='.'||zone_name OR substr(zone_name,-(length(NEW.hostname)+1))='.'||NEW.hostname))
BEGIN SELECT RAISE(ABORT,'hostname namespace conflict: authorized DNS publishing client'); END;
CREATE TRIGGER remote_dns_receiver_namespace_guard
BEFORE INSERT ON remote_dns_zone_ownership
WHEN EXISTS(SELECT 1 FROM hostname_reservations WHERE
    hostname=NEW.zone_name OR substr(hostname,-(length(NEW.zone_name)+1))='.'||NEW.zone_name
    OR substr(NEW.zone_name,-(length(hostname)+1))='.'||hostname)
BEGIN SELECT RAISE(ABORT,'hostname namespace conflict: local hostname reservation'); END;
CREATE TRIGGER pdns_remote_namespace_insert
BEFORE INSERT ON pdns_domains
WHEN EXISTS(SELECT 1 FROM remote_dns_zone_ownership WHERE
    NEW.name=zone_name OR substr(NEW.name,-(length(zone_name)+1))='.'||zone_name
    OR substr(zone_name,-(length(NEW.name)+1))='.'||NEW.name)
AND NOT EXISTS(SELECT 1 FROM remote_dns_zone_ownership WHERE zone_name=NEW.name AND ((generation=0 AND deleted=0) OR (deleted=1 AND applied_generation=generation)))
BEGIN SELECT RAISE(ABORT,'hostname namespace conflict: authorized DNS publishing client'); END;
CREATE TRIGGER pdns_remote_namespace_rename
BEFORE UPDATE OF name ON pdns_domains
WHEN NEW.name!=OLD.name AND EXISTS(SELECT 1 FROM remote_dns_zone_ownership WHERE
    NEW.name=zone_name OR substr(NEW.name,-(length(zone_name)+1))='.'||zone_name
    OR substr(zone_name,-(length(NEW.name)+1))='.'||NEW.name)
BEGIN SELECT RAISE(ABORT,'hostname namespace conflict: authorized DNS publishing client'); END;

CREATE TABLE remote_dns_origin_history (
    connection_id TEXT NOT NULL REFERENCES remote_dns_connections(id),
    zone_name TEXT NOT NULL,
    generation INTEGER NOT NULL,
    applied_generation INTEGER NOT NULL,
    payload_hash TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    deleted INTEGER NOT NULL CHECK(deleted IN(0,1)),
    PRIMARY KEY(connection_id,zone_name)
);
CREATE TRIGGER remote_dns_domain_delete_requires_receipt
BEFORE DELETE ON domains
WHEN OLD.dns_management='existing' AND OLD.parent_domain_id IS NULL AND EXISTS
    (SELECT 1 FROM remote_dns_zones WHERE domain_id=OLD.id AND (deleted=0 OR applied_generation!=generation))
BEGIN SELECT RAISE(ABORT,'Reconcile the exact remote DNS deletion before removing its domain'); END;
