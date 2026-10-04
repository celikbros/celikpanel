import json, sys, os
E = sys.argv[1]
R = os.path.join(E, "raw", "results"); R = R if os.path.exists(os.path.join(R, "reboot-checkpoint-1.json")) else os.path.join(R, os.listdir(R)[0])
cp = json.load(open(os.path.join(R, "reboot-checkpoint-1.json")))
r = cp["result"]
def g(o, *ks):
    for k in ks:
        if not isinstance(o, dict): return None
        o = o.get(k)
    return o
print("status", r.get("status"), "safety", r.get("safety_status"), "classification", g(r, "recovery_outcome", "classification"))
print("gate_probe", json.dumps(r.get("gate_probe"))[:300])
print("host_idle", json.dumps(r.get("host_idle_before_trigger"))[:300])
print("kill", json.dumps({k: g(r, "kill", k) for k in ("exit_code", "state", "delivered_at", "tagged_state")}))
f = r.get("fresh_primary_v3") or {}
print("fresh_primary_v3 keys", sorted(f.keys()))
rs = g(f, "restart") or {}
print("restart keys", sorted(rs.keys()))
fw = g(rs, "forward") or {}
print("forward keys", sorted(fw.keys()))
print("catalog_restamp", json.dumps(fw.get("catalog_restamp"))[:900])
print("native_serial_at_cut", json.dumps(g(f, "native_serial_at_cut") or fw.get("native_serial_at_cut")))
for k in ("pdns_main_pid", "main_pid", "mainpid"):
    if k in fw: print(k, fw[k])
print("release", json.dumps(g(rs, "release"))[:600])
print("retries", json.dumps([{k: x.get(k) for k in ("returncode", "elapsed_seconds", "outcome")} for x in (r.get("retries") or [])])[:400])
print("dns samples", json.dumps(g(r, "dns_only_samples") or g(r, "outage", "samples_summary"))[:300])
print("failures", cp.get("failures"))
pv = json.load(open(os.path.join(E, "fresh-primary-peer", "peer-verdict.json")))
print("peer-verdict", pv["status"], pv["combined_exit"], pv["observation"]["catalog_serial"], pv["observation"]["catalog_members"])
