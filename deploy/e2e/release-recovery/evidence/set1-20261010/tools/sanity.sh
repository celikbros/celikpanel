#!/bin/bash
# read-only sanity of the helper's pure/reader functions on the host (no guard, no owner action)
cd /var/tmp/cp-set1-run/harness-set1/deploy/e2e/release-recovery
PYTHONDONTWRITEBYTECODE=1 python3 - <<'PY'
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("g", "guest_settings_native.py"); g = importlib.util.module_from_spec(spec); spec.loader.exec_module(g)
s = g.file_state("/etc/hostname", text=True); print({k: s[k] for k in ("owner", "group", "mode", "type", "size")}, repr(s.get("text"))[:40])
print(g.file_state("/nonexistent"))
print(g.unit_facts(("systemd-journald.service",)))
print(g.run(["crontab", "-u", "nobody", "-l"]))
print(g.read_clock({}))
try:
    g.read_journal({"units": [], "since_epoch": 0})
except g.Refused as e:
    print("refused ok", e)
print(g.smtp_dialogue(25, None, None))
PY
