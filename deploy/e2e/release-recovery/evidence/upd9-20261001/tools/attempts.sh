#!/bin/bash
# usage: attempts.sh LAB -> setup attempt files of the running/finished cell (read-only)
for f in /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*setup/setup-execution-attempt-*.json; do
  [ -f "$f" ] || continue
  python3 - "$f" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(sys.argv[1][-7:-5], d.get("phase"), json.dumps(d.get("error"), ensure_ascii=False)[:220])
PY
done
ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/ | tr '\n' ' '; echo
