-- Preserve migration 033's operation identity and topology invariants while
-- strengthening its attachment gate now that ordinary V3 publications
-- acquire dns_zone_engine_leases between snapshot preparation and receipt
-- finalization.
DROP TRIGGER dns_engine_state_attach_switch_guard;

CREATE TRIGGER dns_engine_state_attach_switch_guard
BEFORE UPDATE OF current_switch_id ON dns_engine_state
WHEN OLD.current_switch_id IS NULL
 AND NEW.current_switch_id IS NOT NULL
 AND (
    EXISTS (SELECT 1 FROM dns_zone_engine_leases)
    OR NOT EXISTS (
        SELECT 1 FROM dns_engine_switch_snapshots AS snapshot
        WHERE snapshot.switch_id = NEW.current_switch_id
          AND snapshot.phase = 'planned'
          AND snapshot.source_engine IS OLD.active_engine
          AND snapshot.source_epoch = OLD.active_epoch
          AND snapshot.source_state_revision = OLD.revision
          AND snapshot.target_epoch = OLD.active_epoch + 1
          AND (
            (snapshot.mode = 'switch'
              AND OLD.topology = 'standalone'
              AND snapshot.topology = 'standalone')
            OR (snapshot.mode = 'adopt'
              AND OLD.active_engine IS NULL
              AND OLD.active_epoch = 0
              AND OLD.topology = 'standalone'
              AND snapshot.source_engine IS NULL
              AND snapshot.target_engine = 'pdns'
              AND snapshot.source_epoch = 0
              AND snapshot.target_epoch = 1
              AND snapshot.topology IN ('standalone', 'paired'))
          )
    )
 )
BEGIN
    SELECT RAISE(ABORT, 'DNS engine switch conflicts with active publication authority');
END;
