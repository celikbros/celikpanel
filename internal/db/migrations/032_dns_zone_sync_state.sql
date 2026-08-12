-- DNS publication is a per-zone desired-state operation. A zone deletion
-- marker deliberately has no foreign key to pdns_domains or domains: it must
-- survive both cascades until the detached PowerDNS database confirms the
-- exact delete generation.
CREATE TABLE dns_zone_deletion_markers (
    zone_name TEXT NOT NULL PRIMARY KEY
        CHECK (
            length(zone_name) BETWEEN 3 AND 253
            AND zone_name = lower(zone_name)
            AND zone_name = trim(zone_name)
            AND instr(zone_name, '.') > 1
            AND substr(zone_name, -1, 1) <> '.'
            AND zone_name NOT LIKE '.%'
            AND zone_name NOT LIKE '%..%'
            AND zone_name NOT GLOB '*[^a-z0-9.-]*'
        ),
    zone_type TEXT NOT NULL CHECK (zone_type IN ('NATIVE', 'MASTER')),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (
            length(created_at) BETWEEN 1 AND 64
            AND julianday(created_at) IS NOT NULL
        ),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (
            length(updated_at) BETWEEN 1 AND 64
            AND julianday(updated_at) IS NOT NULL
        )
);

CREATE TABLE dns_zone_sync_state (
    zone_name TEXT NOT NULL PRIMARY KEY
        CHECK (
            length(zone_name) BETWEEN 3 AND 253
            AND zone_name = lower(zone_name)
            AND zone_name = trim(zone_name)
            AND instr(zone_name, '.') > 1
            AND substr(zone_name, -1, 1) <> '.'
            AND zone_name NOT LIKE '.%'
            AND zone_name NOT LIKE '%..%'
            AND zone_name NOT GLOB '*[^a-z0-9.-]*'
        ),
    source_domain_id INTEGER,
    desired_generation INTEGER NOT NULL DEFAULT 0
        CHECK (desired_generation >= 0),
    applied_generation INTEGER NOT NULL DEFAULT 0
        CHECK (
            applied_generation >= 0
            AND applied_generation <= desired_generation
        ),
    desired_action TEXT NOT NULL CHECK (desired_action IN ('sync', 'delete')),
    desired_zone_type TEXT NOT NULL
        CHECK (desired_zone_type IN ('NATIVE', 'MASTER')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'applied', 'error')),
    last_error TEXT,
    lease_request_id TEXT,
    lease_owner_id TEXT,
    lease_generation INTEGER,
    lease_action TEXT,
    lease_zone_type TEXT,
    lease_qualifier TEXT,
    lease_expires_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (
            length(created_at) BETWEEN 1 AND 64
            AND julianday(created_at) IS NOT NULL
        ),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (
            length(updated_at) BETWEEN 1 AND 64
            AND julianday(updated_at) IS NOT NULL
        ),
    CHECK (
        (desired_action = 'sync' AND source_domain_id IS NOT NULL AND source_domain_id > 0)
        OR (desired_action = 'delete' AND source_domain_id IS NULL)
    ),
    CHECK (
        (status = 'applied' AND applied_generation = desired_generation)
        OR status IN ('pending', 'error')
    ),
    CHECK (status <> 'applied' OR lease_request_id IS NULL),
    CHECK (
        (status = 'error'
            AND last_error IS NOT NULL
            AND length(last_error) BETWEEN 1 AND 2048)
        OR (status IN ('pending', 'applied') AND last_error IS NULL)
    ),
    CHECK (
        (
            lease_request_id IS NULL
            AND lease_owner_id IS NULL
            AND lease_generation IS NULL
            AND lease_action IS NULL
            AND lease_zone_type IS NULL
            AND lease_qualifier IS NULL
            AND lease_expires_at IS NULL
        )
        OR (
            lease_request_id IS NOT NULL
            AND lease_owner_id IS NOT NULL
            AND lease_generation IS NOT NULL
            AND lease_action IS NOT NULL
            AND lease_zone_type IS NOT NULL
            AND lease_qualifier IS NOT NULL
            AND lease_expires_at IS NOT NULL
            AND length(lease_request_id) = 32
            AND lease_request_id NOT GLOB '*[^a-f0-9]*'
            AND length(lease_owner_id) = 32
            AND lease_owner_id NOT GLOB '*[^a-f0-9]*'
            AND lease_generation >= 0
            AND lease_generation <= desired_generation
            AND lease_action IN ('sync', 'delete')
            AND lease_zone_type IN ('NATIVE', 'MASTER')
            AND length(lease_qualifier) = length('dns-zone-sync/v1:sha256:') + 64
            AND substr(lease_qualifier, 1, length('dns-zone-sync/v1:sha256:')) = 'dns-zone-sync/v1:sha256:'
            AND substr(lease_qualifier, length('dns-zone-sync/v1:sha256:') + 1)
                NOT GLOB '*[^a-f0-9]*'
            AND length(lease_expires_at) BETWEEN 1 AND 64
            AND julianday(lease_expires_at) IS NOT NULL
        )
    )
);

CREATE UNIQUE INDEX idx_dns_zone_sync_state_source_domain
    ON dns_zone_sync_state(source_domain_id)
    WHERE source_domain_id IS NOT NULL;

CREATE UNIQUE INDEX idx_dns_zone_sync_state_lease_request
    ON dns_zone_sync_state(lease_request_id)
    WHERE lease_request_id IS NOT NULL;

CREATE INDEX idx_dns_zone_sync_state_pending
    ON dns_zone_sync_state(status, updated_at, zone_name);

-- Seed before installing any trigger. Every pre-existing full zone starts at
-- one deterministic pending generation regardless of its record count.
INSERT INTO dns_zone_sync_state (
    zone_name,
    source_domain_id,
    desired_generation,
    applied_generation,
    desired_action,
    desired_zone_type,
    status,
    created_at,
    updated_at
)
SELECT
    name,
    id,
    1,
    0,
    'sync',
    upper(trim(type)),
    'pending',
    datetime('now'),
    datetime('now')
FROM pdns_domains
ORDER BY id;

-- A deletion marker may only describe a zone that is already absent from the
-- panel ledger. The pdns_domains AFTER DELETE trigger satisfies this rule and
-- still has OLD.name/OLD.type after record cascades have completed.
CREATE TRIGGER dns_zone_deletion_marker_reject_live_insert
BEFORE INSERT ON dns_zone_deletion_markers
WHEN EXISTS (
    SELECT 1 FROM pdns_domains WHERE name = NEW.zone_name
)
BEGIN
    SELECT RAISE(ABORT, 'DNS deletion marker conflicts with a live zone');
END;

CREATE TRIGGER dns_zone_deletion_marker_name_immutable
BEFORE UPDATE OF zone_name ON dns_zone_deletion_markers
WHEN OLD.zone_name IS NOT NEW.zone_name
BEGIN
    SELECT RAISE(ABORT, 'DNS deletion marker zone name is immutable');
END;

CREATE TRIGGER dns_zone_deletion_marker_reject_live_update
BEFORE UPDATE ON dns_zone_deletion_markers
WHEN EXISTS (
    SELECT 1 FROM pdns_domains WHERE name = NEW.zone_name
)
BEGIN
    SELECT RAISE(ABORT, 'DNS deletion marker conflicts with a live zone');
END;

CREATE TRIGGER dns_zone_deletion_marker_pending_delete_guard
BEFORE DELETE ON dns_zone_deletion_markers
WHEN NOT EXISTS (
        SELECT 1 FROM pdns_domains WHERE name = OLD.zone_name
    )
  AND NOT EXISTS (
        SELECT 1
        FROM dns_zone_sync_state
        WHERE zone_name = OLD.zone_name
          AND desired_action = 'delete'
          AND status = 'applied'
          AND applied_generation = desired_generation
          AND lease_request_id IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'DNS deletion marker is not durably applied');
END;

CREATE TRIGGER dns_zone_deletion_marker_insert_sync_state
AFTER INSERT ON dns_zone_deletion_markers
BEGIN
    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        NEW.zone_name,
        NULL,
        1,
        0,
        'delete',
        NEW.zone_type,
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = NULL,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'delete',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;

CREATE TRIGGER dns_zone_deletion_marker_update_sync_state
AFTER UPDATE OF zone_type ON dns_zone_deletion_markers
WHEN OLD.zone_type IS NOT NEW.zone_type
BEGIN
    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        NEW.zone_name,
        NULL,
        1,
        0,
        'delete',
        NEW.zone_type,
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = NULL,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'delete',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;

-- Deleting an applied tombstone retires its state. Deleting a tombstone as
-- part of a live-zone resurrection instead advances the same row back to a
-- sync generation; it never erases an unapplied remote delete silently.
CREATE TRIGGER dns_zone_deletion_marker_delete_sync_state
AFTER DELETE ON dns_zone_deletion_markers
BEGIN
    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    )
    SELECT
        d.name,
        d.id,
        1,
        0,
        'sync',
        upper(trim(d.type)),
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    FROM pdns_domains AS d
    WHERE d.name = OLD.zone_name
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = excluded.source_domain_id,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');

    DELETE FROM dns_zone_sync_state
    WHERE zone_name = OLD.zone_name
      AND NOT EXISTS (
          SELECT 1 FROM pdns_domains WHERE name = OLD.zone_name
      )
      AND desired_action = 'delete'
      AND status = 'applied'
      AND applied_generation = desired_generation
      AND lease_request_id IS NULL;
END;

CREATE TRIGGER pdns_domains_dns_sync_insert
AFTER INSERT ON pdns_domains
BEGIN
    -- A newly live zone supersedes any older delete intent. The marker delete
    -- trigger first advances that old row; this insert then records the live
    -- pdns_domains mutation itself as a distinct generation.
    DELETE FROM dns_zone_deletion_markers WHERE zone_name = NEW.name;

    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        NEW.name,
        NEW.id,
        1,
        0,
        'sync',
        upper(trim(NEW.type)),
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = excluded.source_domain_id,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;

-- A rename is two desired-state operations: delete the exact old remote zone
-- and publish the exact new one. OLD values remain available after UPDATE,
-- while the old live name is already absent so its marker cannot conflict.
CREATE TRIGGER pdns_domains_dns_sync_rename
AFTER UPDATE ON pdns_domains
WHEN OLD.name IS NOT NEW.name
BEGIN
    INSERT INTO dns_zone_deletion_markers (
        zone_name, zone_type, created_at, updated_at
    ) VALUES (
        OLD.name, upper(trim(OLD.type)), datetime('now'), datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        zone_type = excluded.zone_type,
        updated_at = datetime('now');

    DELETE FROM dns_zone_deletion_markers WHERE zone_name = NEW.name;

    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        NEW.name,
        NEW.id,
        1,
        0,
        'sync',
        upper(trim(NEW.type)),
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = excluded.source_domain_id,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;

CREATE TRIGGER pdns_domains_dns_sync_update
AFTER UPDATE ON pdns_domains
WHEN OLD.name IS NEW.name
 AND (
    OLD.id IS NOT NEW.id
    OR OLD.master IS NOT NEW.master
    OR OLD.last_check IS NOT NEW.last_check
    OR OLD.type IS NOT NEW.type
    OR OLD.notified_serial IS NOT NEW.notified_serial
    OR OLD.account IS NOT NEW.account
    OR OLD.options IS NOT NEW.options
    OR OLD.catalog IS NOT NEW.catalog
 )
BEGIN
    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        NEW.name,
        NEW.id,
        1,
        0,
        'sync',
        upper(trim(NEW.type)),
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = excluded.source_domain_id,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;

-- Foreign-key record cascades run before this AFTER DELETE trigger. Creating
-- the marker here guarantees its delete intent is the final generation in the
-- transaction, while OLD preserves the exact name and zone type.
CREATE TRIGGER pdns_domains_dns_sync_delete
AFTER DELETE ON pdns_domains
BEGIN
    INSERT INTO dns_zone_deletion_markers (
        zone_name, zone_type, created_at, updated_at
    ) VALUES (
        OLD.name, upper(trim(OLD.type)), datetime('now'), datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        zone_type = excluded.zone_type,
        updated_at = datetime('now');
END;

CREATE TRIGGER pdns_records_dns_sync_insert
AFTER INSERT ON pdns_records
BEGIN
    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    )
    SELECT
        d.name,
        d.id,
        1,
        0,
        'sync',
        upper(trim(d.type)),
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    FROM pdns_domains AS d
    WHERE d.id = NEW.domain_id
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = excluded.source_domain_id,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;

CREATE TRIGGER pdns_records_dns_sync_delete
AFTER DELETE ON pdns_records
BEGIN
    UPDATE dns_zone_sync_state
    SET desired_generation = desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = COALESCE(
            (
                SELECT upper(trim(type))
                FROM pdns_domains
                WHERE id = OLD.domain_id
            ),
            desired_zone_type
        ),
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE source_domain_id = OLD.domain_id;
END;

-- Only fields present in the full V2 record tuple affect its generation.
-- A domain_id move advances the old zone and the new zone exactly once each.
CREATE TRIGGER pdns_records_dns_sync_update
AFTER UPDATE ON pdns_records
WHEN OLD.id IS NOT NEW.id
  OR OLD.domain_id IS NOT NEW.domain_id
  OR OLD.name IS NOT NEW.name
  OR OLD.type IS NOT NEW.type
  OR OLD.content IS NOT NEW.content
  OR OLD.ttl IS NOT NEW.ttl
  OR OLD.prio IS NOT NEW.prio
  OR OLD.disabled IS NOT NEW.disabled
  OR OLD.ordername IS NOT NEW.ordername
  OR OLD.auth IS NOT NEW.auth
BEGIN
    UPDATE dns_zone_sync_state
    SET desired_generation = desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = COALESCE(
            (
                SELECT upper(trim(type))
                FROM pdns_domains
                WHERE id = OLD.domain_id
            ),
            desired_zone_type
        ),
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now')
    WHERE source_domain_id = OLD.domain_id;

    INSERT INTO dns_zone_sync_state (
        zone_name,
        source_domain_id,
        desired_generation,
        applied_generation,
        desired_action,
        desired_zone_type,
        status,
        last_error,
        created_at,
        updated_at
    )
    SELECT
        d.name,
        d.id,
        1,
        0,
        'sync',
        upper(trim(d.type)),
        'pending',
        NULL,
        datetime('now'),
        datetime('now')
    FROM pdns_domains AS d
    WHERE d.id = NEW.domain_id
      AND NEW.domain_id IS NOT OLD.domain_id
    ON CONFLICT(zone_name) DO UPDATE SET
        source_domain_id = excluded.source_domain_id,
        desired_generation = dns_zone_sync_state.desired_generation + 1,
        desired_action = 'sync',
        desired_zone_type = excluded.desired_zone_type,
        status = 'pending',
        last_error = NULL,
        updated_at = datetime('now');
END;
