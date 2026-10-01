#!/usr/bin/env python3
"""upd8 read-only: setup attempts per staged cell run (from the staged evidence)."""
import glob, json, sys
root = sys.argv[1]
for f in sorted(glob.glob(root + "/upd1-*/run-*/steps/06-setup/step.json")):
    d = json.load(open(f)); c = d["checks"]
    run = f[len(root) + 1:].split("/steps")[0]
    att = c.get("owner_attempts") or []
    waits = c.get("owner_idle_waits") or []
    print(run, d["verdict"], d["started_at"], (d.get("finished_at") or "")[11:], "attempts", len(att),
          "idle-waited-s", [w.get("waited_s") for w in waits], "busy", sorted({p for w in waits for b in w.get("busy_seen", []) for p in b.get("processes", [])}))
    for a in att:
        comp = a.get("component") or {}
        print("   ", a["attempt"], a.get("status"), a.get("phase"), (a.get("error") or {}).get("code"),
              "component:", comp.get("source") or ("api+log" if comp.get("log_line") else ("api" if comp.get("read") else "-")), "busy-named" if comp.get("names_host_busy") else "")
    if c.get("isolated_host_wait"):
        w = c["isolated_host_wait"]; print("    settled at", w.get("phase"), w.get("code"), "steps", w.get("steps"))
