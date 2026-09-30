# usage: fp.py SHORT CELL -- read-only summary of the fresh-primary parts of result.json
import json, sys
S, C = sys.argv[1], sys.argv[2]
E = f"/var/tmp/cp-b9-1001/evidence/{S}"
r = json.load(open(f"{E}/raw/results/{C}/result.json"))
def d(k, v, n=3000): print(k, "=", json.dumps(v, sort_keys=True, default=str)[:n])
for k in ("status", "safety_status", "request_id", "kill_proven", "scenario_trigger_returncode"):
    d(k, r.get(k))
d("gate_probe", r.get("gate_probe"))
d("boundary", r.get("boundary"))
d("journal_at_boundary", r.get("journal_at_boundary"), 800)
k = r.get("kill") or {}
d("kill", {x: k.get(x) for x in ("pid", "state_before_signal", "delivered_at", "exit_code", "proc_entry_absent_after_reap")})
d("marker", (r.get("boundary_marker") or {}).get("recorded_at"))
fp = r.get("fresh_primary_v3") or {}
d("fresh_primary_v3.failures", fp.get("failures"))
d("fresh_primary_v3.unknown", fp.get("unknown"))
rs = fp.get("restart") or {}
d("restart.failures", rs.get("failures")); d("restart.unknown", rs.get("unknown"))
d("restart.journal_retired", rs.get("journal_retired"))
rel = rs.get("release") or {}
job = rel.get("job") or {}
d("restart.release.job", {x: job.get(x) for x in ("status", "phase", "error_code", "error_message", "attempt", "lease_owner")}, 2000)
d("restart.pre_install", rs.get("pre_install"), 2500)
fw = rs.get("forward") or {}
d("restart.forward.failures", fw.get("failures")); d("restart.forward.unknown", fw.get("unknown"))
d("restart.forward.pdns_pid_history", fw.get("pdns_pid_history"))
d("restart.forward.catalog_restamp", fw.get("catalog_restamp"), 2000)
d("restart.agent_journal_owner_command", rs.get("agent_journal_owner_command"), 800)
d("pair", fp.get("pair"), 2500)
d("cut", r.get("fresh_primary_v3_cut") or fp.get("cut"), 2500)
h = r.get("fresh_primary_v3_hold")
if h:
    d("hold.status", h.get("status")); d("hold.failures", h.get("failures")); d("hold.unknown", h.get("unknown"))
    d("hold.edit", h.get("edit"), 1200)
    rj = (h.get("release") or {}).get("job") or {}
    d("hold.release.job", {x: rj.get(x) for x in ("status", "phase", "error_code", "error_message")}, 3000)
    d("hold.journal_after_release", h.get("journal_after_release"), 600)
    d("hold.unrelated_begin", h.get("unrelated_begin"), 1500)
    orr = h.get("owner_release_recovery")
    if orr:
        d("owner.status", orr.get("status"), 3000); d("owner.command", orr.get("command"), 3000); d("owner.rerun", orr.get("rerun"), 1500)
        d("owner.pre_install", orr.get("pre_install"), 2000); d("owner.ledger", orr.get("ledger"), 800)
for x in ("recovery_outcome", "recovery_status_reads", "reboot_after_recovery"):
    d(x, r.get(x), 2500)
for a in (r.get("recovery") or {}).get("attempts") or []:
    c = a.get("command") or {}
    print(" retry", a.get("ordinal"), "rc", c.get("returncode"), "dur", c.get("duration_seconds"), "err", a.get("error"))
for i, p in enumerate(r.get("recovery_probes") or []):
    print(" probe", i + 1, p.get("recovery_outcome"), (p.get("fingerprint") or "")[:12])
st = r.get("stability") or {}
print("stability samples", st.get("sample_count"), len(st.get("samples") or []))
print("result keys", sorted(r.keys()))
