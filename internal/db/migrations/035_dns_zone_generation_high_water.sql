-- A retired delete tombstone must not erase the generation history for its
-- zone name. Engine receipts are monotonic, so a same-name resurrection must
-- continue at N+1 instead of returning to generation 1.
CREATE TABLE dns_zone_generation_high_water (
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
    generation INTEGER NOT NULL CHECK (generation >= 0),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL)
);

INSERT INTO dns_zone_generation_high_water (zone_name, generation)
SELECT zone_name, max(generation)
FROM (
    SELECT zone_name, desired_generation AS generation
    FROM dns_zone_sync_state
    UNION ALL
    SELECT zone_name, applied_generation AS generation
    FROM dns_zone_engine_applications
)
GROUP BY zone_name;

-- A pre-035 host can already contain the stuck shape this migration repairs:
-- an engine application at delete generation N and a same-name live desired
-- state restarted at generation 1. Never rewrite an in-flight lease or an
-- attached engine snapshot; ambiguity must stop the migration instead.
CREATE TABLE dns_zone_generation_high_water_migration_guard (
    violation INTEGER NOT NULL CHECK (violation = 0)
);

INSERT INTO dns_zone_generation_high_water_migration_guard (violation)
SELECT 1
FROM dns_zone_sync_state AS state
JOIN dns_zone_generation_high_water AS high_water
  ON high_water.zone_name = state.zone_name
WHERE high_water.generation > state.desired_generation
  AND (
      state.lease_request_id IS NOT NULL
      OR EXISTS (
          SELECT 1 FROM dns_zone_engine_leases AS lease
          WHERE lease.zone_name = state.zone_name
      )
      OR EXISTS (
          SELECT 1 FROM dns_engine_state
          WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
      )
  )
LIMIT 1;

UPDATE dns_zone_sync_state
SET desired_generation = (
        SELECT generation + 1
        FROM dns_zone_generation_high_water
        WHERE zone_name = dns_zone_sync_state.zone_name
    ),
    status = 'pending',
    last_error = NULL,
    updated_at = datetime('now')
WHERE lease_request_id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM dns_zone_engine_leases AS lease
      WHERE lease.zone_name = dns_zone_sync_state.zone_name
  )
  AND NOT EXISTS (
      SELECT 1 FROM dns_engine_state
      WHERE singleton_id = 1 AND current_switch_id IS NOT NULL
  )
  AND EXISTS (
      SELECT 1
      FROM dns_zone_generation_high_water AS high_water
      WHERE high_water.zone_name = dns_zone_sync_state.zone_name
        AND high_water.generation > dns_zone_sync_state.desired_generation
  );

INSERT INTO dns_zone_generation_high_water (
    zone_name, generation, created_at, updated_at
)
SELECT zone_name, desired_generation, datetime('now'), datetime('now')
FROM dns_zone_sync_state
WHERE true
ON CONFLICT(zone_name) DO UPDATE SET
    generation = max(
        dns_zone_generation_high_water.generation,
        excluded.generation
    ),
    updated_at = datetime('now');

DROP TABLE dns_zone_generation_high_water_migration_guard;

CREATE TRIGGER dns_zone_generation_high_water_identity_guard
BEFORE UPDATE ON dns_zone_generation_high_water
WHEN NEW.zone_name <> OLD.zone_name
  OR NEW.generation < OLD.generation
BEGIN
    SELECT RAISE(ABORT, 'invalid DNS zone generation high-water transition');
END;

CREATE TRIGGER dns_zone_generation_high_water_delete_guard
BEFORE DELETE ON dns_zone_generation_high_water
BEGIN
    SELECT RAISE(ABORT, 'DNS zone generation high-water is permanent');
END;

-- This one trigger performs both resurrection rebasing and high-water
-- recording in a defined order. It intentionally runs after migration 032's
-- desired-state triggers have created the new state row.
CREATE TRIGGER dns_zone_sync_state_high_water_insert
AFTER INSERT ON dns_zone_sync_state
BEGIN
    UPDATE dns_zone_sync_state
    SET desired_generation = (
            SELECT generation + 1
            FROM dns_zone_generation_high_water
            WHERE zone_name = NEW.zone_name
        ),
        updated_at = datetime('now')
    WHERE zone_name = NEW.zone_name
      AND EXISTS (
          SELECT 1
          FROM dns_zone_generation_high_water
          WHERE zone_name = NEW.zone_name
            AND generation >= NEW.desired_generation
      );

    INSERT INTO dns_zone_generation_high_water (
        zone_name, generation, created_at, updated_at
    )
    SELECT zone_name, desired_generation, datetime('now'), datetime('now')
    FROM dns_zone_sync_state
    WHERE zone_name = NEW.zone_name
    ON CONFLICT(zone_name) DO UPDATE SET
        generation = max(
            dns_zone_generation_high_water.generation,
            excluded.generation
        ),
        updated_at = datetime('now');
END;

CREATE TRIGGER dns_zone_sync_state_high_water_update
AFTER UPDATE OF desired_generation ON dns_zone_sync_state
BEGIN
    INSERT INTO dns_zone_generation_high_water (
        zone_name, generation, created_at, updated_at
    ) VALUES (
        NEW.zone_name, NEW.desired_generation, datetime('now'), datetime('now')
    )
    ON CONFLICT(zone_name) DO UPDATE SET
        generation = max(
            dns_zone_generation_high_water.generation,
            excluded.generation
        ),
        updated_at = datetime('now');
END;
