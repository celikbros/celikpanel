#!/bin/bash
# usage: [DBG=glob] vi.sh LAB NODE -> the two (vi) records of the restore section (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/${DBG:-*}/ | tail -1)
python3 - "$ev" <<'PY'
import json, sys, glob, os
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", "*c8-*", "section.json"))):
    d = json.load(open(path))
    print("restore_seconds", d.get("restore_seconds"))
    for how, r in (d.get("panel_goes_away") or {}).items():
        print("==", how, "|", r["what"])
        print("  client:", r["client_saw"])
        print("  held:", r["held"])
        print("  pids panel", r["panel_main_pid"], "agent", r["agent_main_pid"], "systemctl", r["systemctl"], "rc", r["returncode"])
        print("  ready:", json.dumps(r["panel_ready_again"])[:500])
        ra = r["row_afterwards"] or {}
        print("  row:", ra.get("status"), ra.get("response_status"), ra.get("response_retained"))
        print("  agent left:", r.get("what_the_agent_left"), "| new archives:", r.get("new_archives"), "| partial:", r.get("partial_files"))
        print("  home entries:", r.get("site_home_entries"), "| non-archives:", r.get("backup_directory_entries_that_are_not_archives"))
        print("  replay:", r.get("replay"))
        print("  observations:", [(o["at"][11:], o["classification"][:30], o["archives"]) for o in r.get("observations", [])])
PY
