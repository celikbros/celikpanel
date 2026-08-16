-- DNS engine identity starts unresolved on purpose. Migration must never infer
-- an active authority from installed packages, running units or legacy V2 rows.

CREATE TABLE dns_engine_switch_snapshots (
    switch_id TEXT NOT NULL PRIMARY KEY
        CHECK (
            length(switch_id) = 32
            AND switch_id NOT GLOB '*[^a-f0-9]*'
        ),
    request_id TEXT NOT NULL UNIQUE
        CHECK (
            length(request_id) = 32
            AND request_id NOT GLOB '*[^a-f0-9]*'
        ),
    owner_id TEXT NOT NULL
        CHECK (
            length(owner_id) = 32
            AND owner_id NOT GLOB '*[^a-f0-9]*'
        ),
    source_engine TEXT CHECK (source_engine IN ('pdns', 'bind')),
    target_engine TEXT NOT NULL CHECK (target_engine IN ('pdns', 'bind')),
    source_epoch INTEGER NOT NULL CHECK (source_epoch >= 0),
    target_epoch INTEGER NOT NULL CHECK (target_epoch = source_epoch + 1),
    source_state_revision INTEGER NOT NULL CHECK (source_state_revision >= 0),
    topology TEXT NOT NULL CHECK (topology = 'standalone'),
    phase TEXT NOT NULL DEFAULT 'planned'
        CHECK (phase IN (
            'planned', 'staging', 'staged', 'activating', 'verifying',
            'committed', 'rolling_back', 'rolled_back', 'failed'
        )),
    manifest_qualifier TEXT NOT NULL
        CHECK (
            length(manifest_qualifier) = length('dns-engine-switch/v1:sha256:') + 64
            AND substr(
                manifest_qualifier, 1, length('dns-engine-switch/v1:sha256:')
            ) = 'dns-engine-switch/v1:sha256:'
            AND substr(
                manifest_qualifier, length('dns-engine-switch/v1:sha256:') + 1
            ) NOT GLOB '*[^a-f0-9]*'
        ),
    zone_count INTEGER NOT NULL CHECK (zone_count >= 0 AND zone_count <= 65536),
    snapshot_bytes INTEGER NOT NULL
        CHECK (snapshot_bytes >= 0 AND snapshot_bytes <= 67108864),
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL),
    CHECK (
        (source_engine IS NULL AND source_epoch = 0)
        OR (source_engine IS NOT NULL AND source_epoch >= 1)
    ),
    CHECK (source_engine IS NULL OR source_engine <> target_engine),
    CHECK (
        (phase IN ('rolling_back', 'rolled_back', 'failed')
            AND last_error IS NOT NULL
            AND length(last_error) BETWEEN 1 AND 2048)
        OR (phase NOT IN ('rolling_back', 'rolled_back', 'failed') AND last_error IS NULL)
    )
);

CREATE UNIQUE INDEX idx_dns_engine_switch_one_active
    ON dns_engine_switch_snapshots((1))
    WHERE phase NOT IN ('committed', 'rolled_back', 'failed');

CREATE TABLE dns_engine_state (
    singleton_id INTEGER NOT NULL PRIMARY KEY CHECK (singleton_id = 1),
    active_engine TEXT CHECK (active_engine IN ('pdns', 'bind')),
    active_epoch INTEGER NOT NULL DEFAULT 0 CHECK (active_epoch >= 0),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    topology TEXT NOT NULL DEFAULT 'standalone' CHECK (topology = 'standalone'),
    current_switch_id TEXT REFERENCES dns_engine_switch_snapshots(switch_id),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL),
    CHECK (
        (active_engine IS NULL AND active_epoch = 0)
        OR (active_engine IS NOT NULL AND active_epoch >= 1)
    )
);

INSERT INTO dns_engine_state (
    singleton_id, active_engine, active_epoch, revision, topology,
    current_switch_id, created_at, updated_at
) VALUES (
    1, NULL, 0, 0, 'standalone', NULL, datetime('now'), datetime('now')
);

CREATE TRIGGER dns_engine_state_reject_insert
BEFORE INSERT ON dns_engine_state
BEGIN
    SELECT RAISE(ABORT, 'DNS engine state is a migration-seeded singleton');
END;

CREATE TRIGGER dns_engine_state_reject_delete
BEFORE DELETE ON dns_engine_state
BEGIN
    SELECT RAISE(ABORT, 'DNS engine state cannot be deleted');
END;

CREATE TRIGGER dns_engine_state_monotonic_update
BEFORE UPDATE ON dns_engine_state
WHEN NEW.singleton_id <> OLD.singleton_id
  OR NEW.revision <> OLD.revision + 1
  OR NEW.active_epoch < OLD.active_epoch
  OR (OLD.active_engine IS NOT NULL AND NEW.active_engine IS NULL)
  OR (NEW.active_engine IS OLD.active_engine AND NEW.active_epoch <> OLD.active_epoch)
  OR (NEW.active_engine IS NOT OLD.active_engine AND NEW.active_epoch <= OLD.active_epoch)
  OR (OLD.current_switch_id IS NOT NULL
      AND NEW.current_switch_id IS NOT NULL
      AND NEW.current_switch_id <> OLD.current_switch_id)
BEGIN
    SELECT RAISE(ABORT, 'invalid DNS engine state transition');
END;

CREATE TABLE dns_engine_switch_zones (
    switch_id TEXT NOT NULL
        REFERENCES dns_engine_switch_snapshots(switch_id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0 AND ordinal < 65536),
    zone_name TEXT NOT NULL
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
    desired_generation INTEGER NOT NULL CHECK (desired_generation >= 0),
    desired_action TEXT NOT NULL CHECK (desired_action IN ('sync', 'delete')),
    desired_zone_type TEXT NOT NULL CHECK (desired_zone_type IN ('NATIVE', 'MASTER')),
    zone_qualifier TEXT NOT NULL
        CHECK (
            length(zone_qualifier) = length('dns-zone-sync/v3:sha256:') + 64
            AND substr(zone_qualifier, 1, length('dns-zone-sync/v3:sha256:'))
                = 'dns-zone-sync/v3:sha256:'
            AND substr(zone_qualifier, length('dns-zone-sync/v3:sha256:') + 1)
                NOT GLOB '*[^a-f0-9]*'
        ),
    records_json TEXT NOT NULL
        CHECK (
            length(records_json) BETWEEN 2 AND 8388608
            AND json_valid(records_json)
            AND json_type(records_json) = 'array'
        ),
    records_bytes INTEGER NOT NULL
        CHECK (
            records_bytes >= 2
            AND records_bytes <= 8388608
            AND records_bytes = length(CAST(records_json AS BLOB))
        ),
    phase TEXT NOT NULL DEFAULT 'pending'
        CHECK (phase IN ('pending', 'staged', 'verified', 'error')),
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL),
    PRIMARY KEY (switch_id, zone_name),
    UNIQUE (switch_id, ordinal),
    CHECK (
        (desired_action = 'delete' AND json_array_length(records_json) = 0)
        OR (desired_action = 'sync' AND json_array_length(records_json) > 0)
    ),
    CHECK (
        (phase = 'error' AND last_error IS NOT NULL AND length(last_error) BETWEEN 1 AND 2048)
        OR (phase <> 'error' AND last_error IS NULL)
    )
);

CREATE TRIGGER dns_engine_switch_zone_insert_guard
BEFORE INSERT ON dns_engine_switch_zones
WHEN NOT EXISTS (
    SELECT 1 FROM dns_engine_switch_snapshots
    WHERE switch_id = NEW.switch_id AND phase = 'planned'
)
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch snapshot is frozen');
END;

CREATE TRIGGER dns_engine_switch_zone_identity_immutable
BEFORE UPDATE ON dns_engine_switch_zones
WHEN NEW.switch_id <> OLD.switch_id
  OR NEW.ordinal <> OLD.ordinal
  OR NEW.zone_name <> OLD.zone_name
  OR NEW.desired_generation <> OLD.desired_generation
  OR NEW.desired_action <> OLD.desired_action
  OR NEW.desired_zone_type <> OLD.desired_zone_type
  OR NEW.zone_qualifier <> OLD.zone_qualifier
  OR NEW.records_json <> OLD.records_json
  OR NEW.records_bytes <> OLD.records_bytes
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch zone identity is immutable');
END;

CREATE TRIGGER dns_engine_switch_zone_delete_guard
BEFORE DELETE ON dns_engine_switch_zones
WHEN NOT EXISTS (
    SELECT 1 FROM dns_engine_switch_snapshots
    WHERE switch_id = OLD.switch_id AND phase = 'planned'
)
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch snapshot is frozen');
END;

CREATE TRIGGER dns_engine_switch_snapshot_count_guard
BEFORE UPDATE OF phase ON dns_engine_switch_snapshots
WHEN OLD.phase = 'planned'
 AND NEW.phase = 'staging'
 AND (
    (SELECT count(*) FROM dns_engine_switch_zones WHERE switch_id = OLD.switch_id)
        <> OLD.zone_count
    OR (
        OLD.zone_count > 0
        AND (
            (SELECT min(ordinal) FROM dns_engine_switch_zones WHERE switch_id = OLD.switch_id) <> 0
            OR (SELECT max(ordinal) FROM dns_engine_switch_zones WHERE switch_id = OLD.switch_id)
                <> OLD.zone_count - 1
        )
    )
    OR (SELECT COALESCE(sum(records_bytes), 0)
        FROM dns_engine_switch_zones WHERE switch_id = OLD.switch_id)
        <> OLD.snapshot_bytes
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch snapshot zone count mismatch');
END;

-- V3 authority is deliberately separate from migration 032's PowerDNS-only
-- V1 lease. Mixing the qualifier namespaces would make recovery ambiguous.
CREATE TABLE dns_zone_engine_leases (
    zone_name TEXT NOT NULL PRIMARY KEY
        REFERENCES dns_zone_sync_state(zone_name) ON DELETE RESTRICT,
    engine TEXT NOT NULL CHECK (engine IN ('pdns', 'bind')),
    engine_epoch INTEGER NOT NULL CHECK (engine_epoch >= 1),
    request_id TEXT NOT NULL UNIQUE
        CHECK (length(request_id) = 32 AND request_id NOT GLOB '*[^a-f0-9]*'),
    owner_id TEXT NOT NULL
        CHECK (length(owner_id) = 32 AND owner_id NOT GLOB '*[^a-f0-9]*'),
    desired_generation INTEGER NOT NULL CHECK (desired_generation >= 0),
    desired_action TEXT NOT NULL CHECK (desired_action IN ('sync', 'delete')),
    desired_zone_type TEXT NOT NULL CHECK (desired_zone_type IN ('NATIVE', 'MASTER')),
    qualifier TEXT NOT NULL
        CHECK (
            length(qualifier) = length('dns-zone-sync/v3:sha256:') + 64
            AND substr(qualifier, 1, length('dns-zone-sync/v3:sha256:'))
                = 'dns-zone-sync/v3:sha256:'
            AND substr(qualifier, length('dns-zone-sync/v3:sha256:') + 1)
                NOT GLOB '*[^a-f0-9]*'
        ),
    expires_at TEXT NOT NULL
        CHECK (length(expires_at) BETWEEN 1 AND 64 AND julianday(expires_at) IS NOT NULL),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL)
);

CREATE TRIGGER dns_zone_engine_lease_exact_insert
BEFORE INSERT ON dns_zone_engine_leases
WHEN NOT EXISTS (
    SELECT 1 FROM dns_zone_sync_state AS state
    WHERE state.zone_name = NEW.zone_name
      AND state.desired_generation = NEW.desired_generation
      AND state.desired_action = NEW.desired_action
      AND state.desired_zone_type = NEW.desired_zone_type
      AND state.lease_request_id IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS engine lease does not match desired zone state');
END;

CREATE TRIGGER dns_zone_engine_lease_exact_update
BEFORE UPDATE ON dns_zone_engine_leases
WHEN NEW.zone_name <> OLD.zone_name
 OR NOT EXISTS (
    SELECT 1 FROM dns_zone_sync_state AS state
    WHERE state.zone_name = NEW.zone_name
      AND state.desired_generation = NEW.desired_generation
      AND state.desired_action = NEW.desired_action
      AND state.desired_zone_type = NEW.desired_zone_type
      AND state.lease_request_id IS NULL
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS engine lease does not match desired zone state');
END;

CREATE TRIGGER dns_zone_sync_state_engine_lease_delete_guard
BEFORE DELETE ON dns_zone_sync_state
WHEN EXISTS (
    SELECT 1 FROM dns_zone_engine_leases WHERE zone_name = OLD.zone_name
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone has a live engine-bound lease');
END;

CREATE TRIGGER dns_zone_sync_state_legacy_lease_conflict_guard
BEFORE UPDATE OF lease_request_id ON dns_zone_sync_state
WHEN NEW.lease_request_id IS NOT NULL
 AND EXISTS (
    SELECT 1 FROM dns_zone_engine_leases WHERE zone_name = OLD.zone_name
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS zone already has an engine-bound lease');
END;

-- Latest verified application identity per zone/backend. Rows deliberately do
-- not reference dns_zone_sync_state so a verified remote delete can outlive a
-- retired panel tombstone and still be attributed to its engine and epoch.
CREATE TABLE dns_zone_engine_applications (
    zone_name TEXT NOT NULL,
    engine TEXT NOT NULL CHECK (engine IN ('pdns', 'bind')),
    engine_epoch INTEGER NOT NULL CHECK (engine_epoch >= 1),
    applied_generation INTEGER NOT NULL CHECK (applied_generation >= 0),
    applied_action TEXT NOT NULL CHECK (applied_action IN ('sync', 'delete')),
    applied_zone_type TEXT NOT NULL CHECK (applied_zone_type IN ('NATIVE', 'MASTER')),
    qualifier TEXT NOT NULL
        CHECK (
            length(qualifier) = length('dns-zone-sync/v3:sha256:') + 64
            AND substr(qualifier, 1, length('dns-zone-sync/v3:sha256:'))
                = 'dns-zone-sync/v3:sha256:'
            AND substr(qualifier, length('dns-zone-sync/v3:sha256:') + 1)
                NOT GLOB '*[^a-f0-9]*'
        ),
    mutation_request_id TEXT NOT NULL
        CHECK (
            length(mutation_request_id) = 32
            AND mutation_request_id NOT GLOB '*[^a-f0-9]*'
        ),
    mutation_owner_id TEXT NOT NULL
        CHECK (
            length(mutation_owner_id) = 32
            AND mutation_owner_id NOT GLOB '*[^a-f0-9]*'
        ),
    switch_id TEXT REFERENCES dns_engine_switch_snapshots(switch_id),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(applied_at) BETWEEN 1 AND 64 AND julianday(applied_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL),
    PRIMARY KEY (zone_name, engine),
    CHECK (
        length(zone_name) BETWEEN 3 AND 253
        AND zone_name = lower(zone_name)
        AND zone_name = trim(zone_name)
        AND instr(zone_name, '.') > 1
        AND substr(zone_name, -1, 1) <> '.'
        AND zone_name NOT LIKE '.%'
        AND zone_name NOT LIKE '%..%'
        AND zone_name NOT GLOB '*[^a-z0-9.-]*'
    )
);

CREATE INDEX idx_dns_zone_engine_applications_epoch
    ON dns_zone_engine_applications(engine, engine_epoch, zone_name);

CREATE INDEX idx_dns_zone_engine_applications_request
    ON dns_zone_engine_applications(mutation_request_id, zone_name, engine);

CREATE TRIGGER dns_zone_engine_application_monotonic_update
BEFORE UPDATE ON dns_zone_engine_applications
WHEN NEW.zone_name <> OLD.zone_name
  OR NEW.engine <> OLD.engine
  OR NEW.engine_epoch < OLD.engine_epoch
  OR NEW.applied_generation < OLD.applied_generation
  OR NEW.revision <> OLD.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'invalid DNS zone engine application transition');
END;

CREATE TRIGGER dns_engine_switch_snapshot_identity_immutable
BEFORE UPDATE ON dns_engine_switch_snapshots
WHEN NEW.switch_id <> OLD.switch_id
  OR NEW.request_id <> OLD.request_id
  OR NEW.owner_id <> OLD.owner_id
  OR NEW.source_engine IS NOT OLD.source_engine
  OR NEW.target_engine <> OLD.target_engine
  OR NEW.source_epoch <> OLD.source_epoch
  OR NEW.target_epoch <> OLD.target_epoch
  OR NEW.source_state_revision <> OLD.source_state_revision
  OR NEW.topology <> OLD.topology
  OR NEW.manifest_qualifier <> OLD.manifest_qualifier
  OR NEW.zone_count <> OLD.zone_count
  OR NEW.snapshot_bytes <> OLD.snapshot_bytes
  OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch snapshot identity is immutable');
END;

CREATE TRIGGER dns_engine_switch_snapshot_phase_guard
BEFORE UPDATE OF phase ON dns_engine_switch_snapshots
WHEN NEW.phase <> OLD.phase
 AND NOT (
    (OLD.phase = 'planned' AND NEW.phase IN ('staging', 'failed'))
    OR (OLD.phase = 'staging' AND NEW.phase IN ('staged', 'rolling_back', 'failed'))
    OR (OLD.phase = 'staged' AND NEW.phase IN ('activating', 'rolling_back', 'failed'))
    OR (OLD.phase = 'activating' AND NEW.phase IN ('verifying', 'rolling_back'))
    OR (OLD.phase = 'verifying' AND NEW.phase IN ('committed', 'rolling_back'))
    OR (OLD.phase = 'rolling_back' AND NEW.phase IN ('rolled_back', 'failed'))
 )
BEGIN
    SELECT RAISE(ABORT, 'invalid DNS engine switch phase transition');
END;

CREATE TRIGGER dns_engine_switch_snapshot_reject_delete
BEFORE DELETE ON dns_engine_switch_snapshots
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch history cannot be deleted');
END;

CREATE TRIGGER dns_engine_state_attach_switch_guard
BEFORE UPDATE OF current_switch_id ON dns_engine_state
WHEN OLD.current_switch_id IS NULL
 AND NEW.current_switch_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1 FROM dns_engine_switch_snapshots AS snapshot
    WHERE snapshot.switch_id = NEW.current_switch_id
      AND snapshot.phase = 'planned'
      AND snapshot.source_engine IS OLD.active_engine
      AND snapshot.source_epoch = OLD.active_epoch
      AND snapshot.source_state_revision = OLD.revision
      AND snapshot.target_epoch = OLD.active_epoch + 1
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch does not match singleton state');
END;

CREATE TRIGGER dns_engine_state_detach_switch_guard
BEFORE UPDATE OF current_switch_id ON dns_engine_state
WHEN OLD.current_switch_id IS NOT NULL
 AND NEW.current_switch_id IS NULL
 AND NOT EXISTS (
    SELECT 1 FROM dns_engine_switch_snapshots AS snapshot
    WHERE snapshot.switch_id = OLD.current_switch_id
      AND (
        (snapshot.phase = 'committed'
          AND NEW.active_engine = snapshot.target_engine
          AND NEW.active_epoch = snapshot.target_epoch)
        OR (snapshot.phase IN ('rolled_back', 'failed')
          AND NEW.active_engine IS OLD.active_engine
          AND NEW.active_epoch = OLD.active_epoch)
      )
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch is not safely terminal');
END;

CREATE TRIGGER dns_engine_state_engine_change_guard
BEFORE UPDATE OF active_engine, active_epoch ON dns_engine_state
WHEN (NEW.active_engine IS NOT OLD.active_engine OR NEW.active_epoch <> OLD.active_epoch)
 AND NOT (
    OLD.current_switch_id IS NOT NULL
    AND NEW.current_switch_id IS NULL
    AND EXISTS (
        SELECT 1 FROM dns_engine_switch_snapshots AS snapshot
        WHERE snapshot.switch_id = OLD.current_switch_id
          AND snapshot.phase = 'committed'
          AND snapshot.target_engine = NEW.active_engine
          AND snapshot.target_epoch = NEW.active_epoch
    )
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS engine identity can change only through a committed switch');
END;

-- The switch snapshot is authoritative from attachment through terminal
-- verification. No domain/record writer may change the ledger behind that
-- frozen manifest while the host cutover is in flight.
CREATE TRIGGER dns_engine_switch_freeze_domain_insert
BEFORE INSERT ON pdns_domains
WHEN EXISTS (
    SELECT 1 FROM dns_engine_state
    WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone ledger is frozen during engine switch');
END;

CREATE TRIGGER dns_engine_switch_freeze_domain_update
BEFORE UPDATE ON pdns_domains
WHEN EXISTS (
    SELECT 1 FROM dns_engine_state
    WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone ledger is frozen during engine switch');
END;

CREATE TRIGGER dns_engine_switch_freeze_domain_delete
BEFORE DELETE ON pdns_domains
WHEN EXISTS (
    SELECT 1 FROM dns_engine_state
    WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone ledger is frozen during engine switch');
END;

CREATE TRIGGER dns_engine_switch_freeze_record_insert
BEFORE INSERT ON pdns_records
WHEN EXISTS (
    SELECT 1 FROM dns_engine_state
    WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone ledger is frozen during engine switch');
END;

CREATE TRIGGER dns_engine_switch_freeze_record_update
BEFORE UPDATE ON pdns_records
WHEN EXISTS (
    SELECT 1 FROM dns_engine_state
    WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone ledger is frozen during engine switch');
END;

CREATE TRIGGER dns_engine_switch_freeze_record_delete
BEFORE DELETE ON pdns_records
WHEN EXISTS (
    SELECT 1 FROM dns_engine_state
    WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'DNS zone ledger is frozen during engine switch');
END;
