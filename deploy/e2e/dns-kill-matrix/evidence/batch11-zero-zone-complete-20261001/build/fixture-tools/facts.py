# Batch 11, read-only: verdict summary per cell from the collected evidence. Arg: evidence dir
import json, sys, os, glob
E = sys.argv[1]
R = glob.glob(os.path.join(E, "raw", "results", "*"))[0]
rp = os.path.join(R, "result.json")
if os.path.exists(rp):
    r = json.load(open(rp)); src = "result.json"
else:
    r = json.load(open(os.path.join(R, "reboot-checkpoint-1.json")))["result"]; src = "reboot-checkpoint-1.json result"
def g(o, *ks):
    for k in ks:
        if not isinstance(o, dict): return None
        o = o.get(k)
    return o
print("source", src)
print("status", r.get("status"), "safety", r.get("safety_status"), "kill_proven", r.get("kill_proven"), "classification", g(r, "recovery_outcome", "classification"))
print("kill", json.dumps({k: g(r, "kill", k) for k in ("exit_code", "delivered_at", "marker_to_sigkill_ms")}))
print("boundary", json.dumps({k: g(r, "boundary", k) for k in ("journal_phase", "edge")}), "marker", g(r, "boundary_marker", "recorded_at"))
print("complete_verdict", json.dumps(r.get("complete_verdict"))[:400])
f = r.get("fresh_primary_v3") or {}
def find(o, key, path=""):
    if isinstance(o, dict):
        for k, v in o.items():
            if k == key: print(path + "." + k, "=", json.dumps(v)[:500])
            find(v, key, path + "." + k)
    elif isinstance(o, list):
        for i, v in enumerate(o): find(v, key, f"{path}[{i}]")
for k in ("catalog_restamp", "serial_rule", "gate_probe", "host_idle_before_trigger"):
    find(r, k)
print("failures", r.get("failures"), r.get("verification_failures"), r.get("safety_failures"))
print("reboot_after_recovery", json.dumps(r.get("reboot_after_recovery"))[:400])
for fn in sorted(glob.glob(os.path.join(E, "fresh-primary-peer", "*.json"))):
    d = json.load(open(fn))
    keep = {k: d.get(k) for k in ("status", "combined_exit", "guest_controller_exit", "guest_controller_suspended_for_zone_lifecycle", "zero_zones", "error") if k in d}
    ob = d.get("observation") or {}
    if ob: keep["observation"] = {k: ob.get(k) for k in ("catalog_serial", "catalog_members", "rndc_status_ok")}
    if "held" in d or "lifecycle_status" in d: keep.update({k: d.get(k) for k in ("lifecycle_status", "peer_verdict_exit", "lifecycle_exit", "next_step")})
    print(os.path.basename(fn), json.dumps(keep)[:700])
