import json, sys
S, C = sys.argv[1:3]
r = json.load(open(f"/var/tmp/cp-b8r-1001/evidence/{S}/raw/results/{C}/result.json"))
rb = r.get("reboot_after_recovery") or {}
print("keys", list(rb.keys()))
for k in ("fresh_primary_child", "management_units", "window", "stability", "post_reboot_window", "dns_alone"):
    if k in rb: print(k, json.dumps(rb[k], sort_keys=True)[:1500])
p = rb.get("fresh_primary_pair") or {}
print("pair after reboot primary", json.dumps(p.get("primary")), "secondary", json.dumps(p.get("secondary")), "state", p.get("state_catalog_serial"))
print("zone_lifecycle (result)", json.dumps(r.get("zone_lifecycle") or r.get("zone_lifecycle_before_reboot"), sort_keys=True)[:2500])
md = r.get("management_disabled_before_reboot") or {}
print("mgmt disabled", json.dumps({k: md.get(k) for k in ("failures", "unknown")}), json.dumps((md.get("units") or {}).get("units"))[:600])
st = r.get("stability") or {}
print("stability", json.dumps({k: st.get(k) for k in st if k != "samples"}, sort_keys=True)[:800])
for k in r:
    if "window" in k or "after_reboot" in k or "post_reboot" in k: print("top", k, json.dumps(r[k], sort_keys=True)[:800])
