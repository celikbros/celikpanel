-- Directional pair identity is deployment authority, not engine-local state.
-- A later BIND <-> PowerDNS switch may advance the active engine epoch and
-- replace the source switch receipt, but it must preserve the exact role and
-- local/peer address/name tuple established by the first committed switch.

CREATE TRIGGER dns_bind_pair_switch_preserve_active_identity
BEFORE INSERT ON dns_bind_pair_switches
WHEN EXISTS (
    SELECT 1
    FROM dns_bind_pair_state AS active
    WHERE active.singleton_id = 1
      AND (
          NEW.pair_role IS NOT active.pair_role
          OR NEW.local_ip IS NOT active.local_ip
          OR NEW.local_ns IS NOT active.local_ns
          OR NEW.peer_ip IS NOT active.peer_ip
          OR NEW.peer_ns IS NOT active.peer_ns
      )
)
BEGIN
    SELECT RAISE(ABORT, 'DNS pair identity cannot change during an engine switch');
END;

CREATE TRIGGER dns_bind_pair_state_identity_immutable
BEFORE UPDATE OF pair_role, local_ip, local_ns, peer_ip, peer_ns
ON dns_bind_pair_state
WHEN NEW.pair_role IS NOT OLD.pair_role
  OR NEW.local_ip IS NOT OLD.local_ip
  OR NEW.local_ns IS NOT OLD.local_ns
  OR NEW.peer_ip IS NOT OLD.peer_ip
  OR NEW.peer_ns IS NOT OLD.peer_ns
BEGIN
    SELECT RAISE(ABORT, 'DNS pair identity is immutable after activation');
END;
