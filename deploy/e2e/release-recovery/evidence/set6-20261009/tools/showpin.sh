#!/bin/bash
# set6 read-only: the name-pinning step of one lab in short. usage: showpin.sh LABNAME
f=$(ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/01-set6-name-pinning/step.json 2>/dev/null | tail -n 1)
[ -f "$f" ] || { echo "no pinning step yet"; exit 0; }
python3 -I - "$f" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
c = s["checks"]
print(s["verdict"], s.get("reason"))
for k in ("nsswitch_hosts", "systemd_resolved", "resolved_total_transactions", "product_paths_present", "uptime_seconds", "at", "written_at", "default_path_settle_seconds", "appended_lines"):
    print(k, "=", json.dumps(c.get(k)))
for n, v in c["loopback_only"].items():
    print(n, v, "|", json.dumps(c["default_path_answers"].get(n)))
PY
g=$(dirname "$f")/name-pinning.json
python3 -I -c "import json,sys;d=json.load(open(sys.argv[1]));print(json.dumps(d.get('resolv_conf')), d.get('resolv_conf_target'));print(d.get('resolved_statistics_after_the_default_path',{}) and d['resolved_statistics_after_the_default_path'].get('stdout'));print(d['hosts_lines'])" "$g"
