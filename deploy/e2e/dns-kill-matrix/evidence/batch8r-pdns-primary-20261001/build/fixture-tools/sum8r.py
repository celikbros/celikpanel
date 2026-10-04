# Read-only per-cell summary from the collected evidence. usage: sum8r.py SHORT CELL
import json, sys, os, glob
S, C = sys.argv[1:3]
E = f"/var/tmp/cp-b8r-1001/evidence/{S}"
def load(p):
    try: return json.load(open(p))
    except Exception as e: return {"_error": f"{type(e).__name__}: {e}"}
def j(v, n=900): return json.dumps(v, sort_keys=True)[:n]
rc = open(f"{E}/run-prepared.rc").read().strip() if os.path.exists(f"{E}/run-prepared.rc") else "?"
print(f"#### {S} {C} {rc}")
r = load(f"{E}/raw/results/{C}/result.json")
print("status", r.get("status"), "safety", r.get("safety_status"), "kill_proven", r.get("kill_proven"), "exit", r.get("exit_code"))
for k in ("failures", "verification_failures", "safety_failures", "diagnostic_failures"):
    if r.get(k): print(k, j(r.get(k), 3000))
b = r.get("boundary") or {}; k = r.get("kill") or {}
print("boundary", j(b), "| kill", k.get("state_before_signal"), k.get("exit_code"), k.get("delivered_at"), "| marker", (r.get("boundary_marker") or {}).get("recorded_at"))
print("gate_probe", (r.get("gate_probe") or {}).get("gate"), "|", (r.get("gate_probe") or {}).get("detail"))
print("host_idle", j(r.get("host_idle_before_trigger"), 1500))
f = r.get("fresh_primary_v3") or {}
print("fp3 variant", f.get("variant"), "failures", j(f.get("failures"), 2000), "unknown", j(f.get("unknown")))
rs = f.get("restart") or {}
print("restart", j({x: rs.get(x) for x in ("variant", "failures", "unknown", "journal_retired", "release", "forward", "agent_journal_owner_command")}, 2500))
p = f.get("pair") or {}
print("pair", j({x: p.get(x) for x in ("expected", "query_targets", "failures", "unknown", "secondary_attempts", "state_catalog_serial")}, 1500))
cut = r.get("fresh_primary_v3_cut") or {}
print("cut", j({"phase": (cut.get("journal") or {}).get("phase"), "sealed": (cut.get("journal") or {}).get("candidate_sealed"), "native_serial": (cut.get("journal") or {}).get("native_serial"), "pdns_main_pid": cut.get("pdns_main_pid"), "unit": cut.get("pdns_unit")}))
rb = r.get("reboot_after_recovery") or {}
if rb: print("reboot", j({x: rb.get(x) for x in ("run", "status", "reason", "failures", "unknown")}, 1500), "| pair after", j((rb.get("fresh_primary_pair") or {}).get("failures")), j((rb.get("fresh_primary_pair") or {}).get("unknown")))
for x in ("complete_verdict", "pre_reboot_verdict"):
    if r.get(x): print(x, j(r.get(x), 1200))
oe = {x: r.get(x) for x in r if "owner" in x}
if oe: print("owner keys", list(oe.keys()))
for x in oe:
    v = r.get(x)
    if isinstance(v, dict): print(x, j({y: v.get(y) for y in ("status", "failures", "unknown", "ambiguities", "run") if y in v}, 1500))
sr = r.get("recovery_status_reads") or {}
for st, v in sr.items():
    cm = (v or {}).get("command") or {}
    print("status_read", st, "rc", cm.get("returncode"), "|", (cm.get("output") or "")[:300].replace("\n", " / "))
for pv in sorted(glob.glob(f"{E}/fresh-primary-peer/*.json")):
    v = load(pv)
    print(os.path.basename(pv), j({x: v.get(x) for x in ("status", "error", "combined_exit", "child_step", "guest_controller_exit", "recover_delete")}, 600))
    if "steps" in v:
        for st in v["steps"]:
            res = st.get("result") or {}
            print("  step", st.get("step"), st.get("verdict"), "rc", st.get("returncode"), "outcome", res.get("outcome"), "job", res.get("job_status"), res.get("job_error_code"), "| obs_err", st.get("observation_error"))
h = r.get("fresh_primary_v3_hold")
if h:
    job = (h.get("release") or {}).get("job") or {}
    print("hold", j({x: h.get(x) for x in ("status", "variant", "owner_edit", "failures", "unknown", "dns_judged")}, 800))
    print("hold edit", j(h.get("edit"), 800))
    print("hold job", job.get("status"), job.get("error_code"), "| journal", (h.get("journal_after_release") or {}).get("sha256"), "| unrelated", (h.get("unrelated_begin") or {}).get("verdict"))
    print("hold release_message:", h.get("release_message"))
    for x in h:
        if x not in ("agent_ready", "agent_restart", "edit", "failures", "files_after_release", "journal_after_release", "owner_edit", "panel", "panel_restart", "release", "release_message", "status", "unknown", "unrelated_begin", "variant", "dns_judged", "dns_not_judged_reason"):
            print("hold extra", x, j(h.get(x), 1500))
