-- Directional pairing is additive so the released engine tables and their historical
-- receipts remain byte-for-byte compatible.  The core engine singleton keeps
-- its released standalone storage topology while these epoch-bound rows carry
-- the stronger primary/secondary identity.

CREATE TABLE dns_bind_pair_switches (
    switch_id TEXT NOT NULL PRIMARY KEY
        REFERENCES dns_engine_switch_snapshots(switch_id),
    pair_role TEXT NOT NULL CHECK (pair_role IN ('primary', 'secondary')),
    local_ip TEXT NOT NULL CHECK (
        length(local_ip) BETWEEN 7 AND 15
        AND local_ip NOT GLOB '*[^0-9.]*'
        AND length(local_ip) - length(replace(local_ip, '.', '')) = 3
    ),
    local_ns TEXT NOT NULL CHECK (
        length(local_ns) BETWEEN 3 AND 253
        AND local_ns = lower(local_ns)
        AND local_ns = trim(local_ns)
        AND local_ns NOT GLOB '*[^a-z0-9.-]*'
        AND instr(local_ns, '.') > 1
    ),
    peer_ip TEXT NOT NULL CHECK (
        length(peer_ip) BETWEEN 7 AND 15
        AND peer_ip NOT GLOB '*[^0-9.]*'
        AND length(peer_ip) - length(replace(peer_ip, '.', '')) = 3
        AND peer_ip <> local_ip
    ),
    peer_ns TEXT NOT NULL CHECK (
        length(peer_ns) BETWEEN 3 AND 253
        AND peer_ns = lower(peer_ns)
        AND peer_ns = trim(peer_ns)
        AND peer_ns NOT GLOB '*[^a-z0-9.-]*'
        AND instr(peer_ns, '.') > 1
        AND peer_ns <> local_ns
    ),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL)
);

CREATE TRIGGER dns_bind_pair_switch_insert_guard
BEFORE INSERT ON dns_bind_pair_switches
WHEN NOT EXISTS (
    SELECT 1 FROM dns_engine_switch_snapshots AS snapshot
    WHERE snapshot.switch_id = NEW.switch_id
      AND snapshot.mode = 'switch'
      AND snapshot.target_engine IN ('bind', 'pdns')
      AND snapshot.topology = 'standalone'
      AND snapshot.phase = 'planned'
)
BEGIN
    SELECT RAISE(ABORT, 'BIND pair identity does not match a planned BIND switch');
END;

CREATE TRIGGER dns_bind_pair_switch_immutable
BEFORE UPDATE ON dns_bind_pair_switches
BEGIN
    SELECT RAISE(ABORT, 'BIND pair switch identity is immutable');
END;

CREATE TRIGGER dns_bind_pair_switch_reject_delete
BEFORE DELETE ON dns_bind_pair_switches
BEGIN
    SELECT RAISE(ABORT, 'BIND pair switch history cannot be deleted');
END;

CREATE TABLE dns_bind_pair_state (
    singleton_id INTEGER NOT NULL PRIMARY KEY CHECK (singleton_id = 1),
    active_epoch INTEGER NOT NULL CHECK (active_epoch >= 1),
    pair_role TEXT NOT NULL CHECK (pair_role IN ('primary', 'secondary')),
    local_ip TEXT NOT NULL,
    local_ns TEXT NOT NULL,
    peer_ip TEXT NOT NULL,
    peer_ns TEXT NOT NULL,
    source_switch_id TEXT NOT NULL UNIQUE
        REFERENCES dns_bind_pair_switches(switch_id),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(created_at) BETWEEN 1 AND 64 AND julianday(created_at) IS NOT NULL),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        CHECK (length(updated_at) BETWEEN 1 AND 64 AND julianday(updated_at) IS NOT NULL)
);

CREATE TRIGGER dns_bind_pair_state_insert_guard
BEFORE INSERT ON dns_bind_pair_state
WHEN NOT EXISTS (
    SELECT 1
    FROM dns_engine_state AS state
    JOIN dns_engine_switch_snapshots AS snapshot
      ON snapshot.switch_id = state.current_switch_id
    JOIN dns_bind_pair_switches AS pairing
      ON pairing.switch_id = snapshot.switch_id
    WHERE state.singleton_id = 1
      AND snapshot.switch_id = NEW.source_switch_id
      AND snapshot.phase = 'committed'
      AND snapshot.target_engine IN ('bind', 'pdns')
      AND snapshot.target_epoch = NEW.active_epoch
      AND pairing.pair_role = NEW.pair_role
      AND pairing.local_ip = NEW.local_ip
      AND pairing.local_ns = NEW.local_ns
      AND pairing.peer_ip = NEW.peer_ip
      AND pairing.peer_ns = NEW.peer_ns
)
BEGIN
    SELECT RAISE(ABORT, 'BIND pair state lacks an exact committed switch');
END;

CREATE TRIGGER dns_bind_pair_state_reject_update
BEFORE UPDATE ON dns_bind_pair_state
WHEN NOT EXISTS (
    SELECT 1
    FROM dns_engine_state AS state
    JOIN dns_engine_switch_snapshots AS snapshot
      ON snapshot.switch_id = state.current_switch_id
    JOIN dns_bind_pair_switches AS pairing
      ON pairing.switch_id = snapshot.switch_id
    WHERE state.singleton_id = 1
      AND snapshot.switch_id = NEW.source_switch_id
      AND snapshot.phase = 'committed'
      AND snapshot.target_engine IN ('bind', 'pdns')
      AND snapshot.target_epoch = NEW.active_epoch
      AND pairing.pair_role = NEW.pair_role
      AND pairing.local_ip = NEW.local_ip
      AND pairing.local_ns = NEW.local_ns
      AND pairing.peer_ip = NEW.peer_ip
      AND pairing.peer_ns = NEW.peer_ns
)
BEGIN
    SELECT RAISE(ABORT, 'DNS pair state update lacks an exact committed switch');
END;

CREATE TRIGGER dns_bind_pair_state_reject_delete
BEFORE DELETE ON dns_bind_pair_state
BEGIN
    SELECT RAISE(ABORT, 'BIND pair state cannot be deleted implicitly');
END;

-- Migration 034's lease guard is preserved and its source-topology rule is
-- extended only for an exact BIND pair row.  The switch snapshot itself stays
-- in the released standalone storage shape.
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
              AND snapshot.topology = 'standalone'
              AND NOT EXISTS (
                  SELECT 1 FROM dns_bind_pair_switches AS pairing
                  WHERE pairing.switch_id = snapshot.switch_id
              ))
            OR (snapshot.mode = 'switch'
              AND snapshot.target_engine IN ('bind', 'pdns')
              AND snapshot.topology = 'standalone'
              AND EXISTS (
                  SELECT 1 FROM dns_bind_pair_switches AS pairing
                  WHERE pairing.switch_id = snapshot.switch_id
              )
              AND (
                  (OLD.active_engine IS NULL AND OLD.topology = 'standalone')
                  OR (OLD.active_engine = 'pdns' AND OLD.topology = 'paired')
                  OR (OLD.active_engine IN ('pdns', 'bind')
                    AND OLD.topology = 'standalone'
                    AND EXISTS (SELECT 1 FROM dns_bind_pair_state))
              ))
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
