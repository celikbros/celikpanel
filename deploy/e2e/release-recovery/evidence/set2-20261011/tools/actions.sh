#!/bin/bash
# usage: actions.sh LAB NODE -> the S8 action table (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 - "$ev" <<'PY'
import json, sys, glob, os
for path in glob.glob(os.path.join(sys.argv[1], "steps", "*s8-*", "section.json")):
    d = json.load(open(path))
    for a in d.get("actions", []):
        v = a.get("vars") or {}
        print(f"{a['service']:10} {a['action']:7} {a['situation'][:34]:34} {a['status']} {str(a['code'] or '')[:22]:22} {str(a['reason'] or ''):8} said={a['said']:7} truth={str(a['truth_action_took_effect']):5} match={a['matches']} pid {a['daemon_before']['pid']}>{a['daemon_after']['pid']} {a['answer'].get('applied') or ''} {a['answer'].get('unit') or v.get('owner_unit') or ''} ev={a['reload_evidence']} | {str(v.get('detail') or '')[:150]}")
PY
