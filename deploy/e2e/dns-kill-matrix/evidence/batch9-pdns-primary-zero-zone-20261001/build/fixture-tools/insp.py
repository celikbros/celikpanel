# Read-only summary of one cell's retained result.json. usage: insp.py SHORT CELL
import json, sys
s, c = sys.argv[1:3]
p = f"/var/tmp/cp-b9-1001/evidence/{s}/raw/results/{c}/result.json"
r = json.load(open(p))
def g(d, *ks):
    for k in ks:
        if not isinstance(d, dict): return None
        d = d.get(k)
    return d
print("status", r.get("status"), "safety", r.get("safety_status"), "request", r.get("request_id"))
print("failures", json.dumps(r.get("failures"))[:2000])
fp = r.get("fresh_primary_v3") or {}
print("fresh_primary_v3 keys", sorted(fp.keys()))
pair = fp.get("pair") or {}
print("pair keys", sorted(pair.keys()))
print("pair.failures", pair.get("failures"))
print("pair.serial_rule", json.dumps(pair.get("serial_rule")))
cat = pair.get("catalog") or {}
print("pair.catalog keys", sorted(cat.keys()) if isinstance(cat, dict) else cat)
if isinstance(cat, dict):
    for k, v in (cat.get("checks") or {}).items():
        print("  check", k, json.dumps(v)[:700])
    print("  database", json.dumps(cat.get("database"))[:2500])
print("catalog_restamp", json.dumps(fp.get("catalog_restamp"))[:1500])
rb = r.get("reboot_after_recovery") or {}
print("reboot status", rb.get("status"), "failures", rb.get("failures"))
fpp = rb.get("fresh_primary_pair") or {}
print("reboot pair failures", fpp.get("failures"), "serial_rule", json.dumps(fpp.get("serial_rule")))
rc = fpp.get("catalog") or {}
if isinstance(rc, dict):
    for k, v in (rc.get("checks") or {}).items():
        print("  rb check", k, json.dumps(v)[:700])
    print("  rb database", json.dumps(rc.get("database"))[:2000])
print("fresh_primary_child", json.dumps(rb.get("fresh_primary_child"))[:800])
print("hold", json.dumps(r.get("fresh_primary_v3_hold"))[:1500])
print("gate", json.dumps(r.get("gate_probe") or fp.get("gate_probe"))[:400])
print("idle", json.dumps(r.get("host_idle_before_trigger"))[:400])
