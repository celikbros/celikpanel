#!/bin/bash
# set6 read-only: the checks of one section of one lab that are not PASS, with their detail (bounded).
# usage: showcheck.sh LABNAME SECTION-SLUG [NAME-PATTERN] [CHARS]
f=$(ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*-$2/section.json 2>/dev/null | tail -n 1)
[ -f "$f" ] || { echo "no such section"; exit 0; }
python3 -I - "$f" "${3:-}" "${4:-3000}" <<'PY'
import json, re, sys
d = json.load(open(sys.argv[1]))
print(d["verdict"], "| error:", d.get("error"), "| keys:", sorted(d))
for c in d["checks"]:
    chosen = re.search(sys.argv[2], c["name"]) if sys.argv[2] else c["ok"] is not True
    print(" ", c["ok"], c["name"][:170])
    if chosen:
        print("     detail:", json.dumps(c["detail"], sort_keys=True)[:int(sys.argv[3])])
PY
