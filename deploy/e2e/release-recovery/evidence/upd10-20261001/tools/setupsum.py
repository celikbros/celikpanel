import json, sys, glob
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{sys.argv[1]}/evidence/*/upd1/*/"))[-1]
d = json.load(open(glob.glob(ev + "steps/*-setup/step.json")[0]))
c = d["checks"]
print("setup", d["verdict"], d["started_at"], d["finished_at"])
waits = {w.get("before_attempt"): w for w in c.get("owner_idle_waits", [])}
for a in c.get("owner_attempts", []):
    w = waits.get(a["attempt"], {})
    comp = a.get("component") or {}
    print(a["attempt"], a.get("phase"), (a.get("error") or {}).get("code"), "| wait before:", w.get("waited_s"), w.get("at"),
          "busy_seen:", [x.get("processes") for x in w.get("busy_seen", [])][:2], "| comp:", comp.get("source") or comp.get("status"), str(comp.get("error") or comp.get("line") or "")[:120])
print("isolated wait:", json.dumps(c.get("isolated_host_wait"))[:300])
print("packagekit:", json.dumps(c.get("packagekit"))[:900])
r = json.load(open(ev + "result.json"))
print("result:", r["overall"], r["outcome"]["classification"], r.get("request_id"))
for s in r["steps"]:
    print("  ", s["name"], s["verdict"], s["started_at"], s["finished_at"], (s.get("reason") or "")[:150])
print("findings:", json.dumps(r["findings"], ensure_ascii=False)[:3000])
