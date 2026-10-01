#!/usr/bin/env python3
"""upd10: read-only view of the busystart records (refusal during the task, idle-alive start)."""
import glob, json, sys
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{sys.argv[1]}/evidence/*/upd1/*/"))[-1]
b = json.load(open(glob.glob(ev + "steps/*busy-start/busy-start-refusal.json")[0]))
s = json.load(open(glob.glob(ev + "steps/*busy-start/step.json")[0]))
c = s["checks"]
print("busy-start", s["verdict"], s["started_at"], s["finished_at"])
op = c.get("operation") or {}
print("operation", json.dumps({k: op.get(k) for k in ("package", "size_bytes", "dl_limit_kib_s", "started_utc", "holding_after_s", "ended_utc", "duration_s")}))
print("unit", json.dumps(op.get("unit")))
print("holding", json.dumps({k: (op.get("holding") or {}).get(k) for k in ("at", "other_package_processes", "general_lock_lines")})[:900])
o = b["observation"]; print("readiness during task", json.dumps(o.get("readiness")))
print("version_before", b["version_before"]); print("units_before", b["units_before"])
print("check", json.dumps(b["check"])[:500])
print("START", json.dumps(b["start"], ensure_ascii=False))
print("after_start reading", json.dumps({k: (b["after_start"].get("before") or {}).get(k) for k in ("at", "other_package_processes", "packagekitd")})[:600])
print("STATUS", json.dumps(b["status"]))
print("owner_texts", json.dumps(b["owner_texts"], ensure_ascii=False, indent=1))
print("panel_log", b["panel_log"]); print("agent_log", b["agent_log"])
print("version_after", b["version_after"]); print("units_after", b["units_after"])
print("expectations", b["expectations"])
a = c.get("after_op") or {}
print("after_op", json.dumps({"label": a.get("label"), "readiness": a.get("readiness"), "pk": [(d["pid"], d["etime_s"], d["backend_pathnames"], d["apt_backend"], d["children"], d["holds_or_waits"], d["rule_idle"]) for d in (a.get("before") or {}).get("packagekitd", [])], "others": (a.get("before") or {}).get("other_package_processes")}))
print("packagekit summary", json.dumps(c.get("packagekit"))[:1200])
for name in ("arm", "owner-start"):
    f = glob.glob(ev + f"steps/*-{name}/step.json")
    if f:
        d = json.load(open(f[0])); print("==", name, d["verdict"], d["started_at"], d["finished_at"])
        for k in ("packagekit_at_arm", "idle_alive_attempt", "start", "readiness", "rearm"):
            if k in d["checks"]:
                print(" ", k, json.dumps(d["checks"][k], ensure_ascii=False)[:2500])
