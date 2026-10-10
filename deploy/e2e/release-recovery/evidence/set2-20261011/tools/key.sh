#!/bin/bash
# usage: key.sh LAB NODE SECTION-GLOB KEY [MAX] -> one key of a section.json (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 - "$ev" "$3" "$4" "${5:-3000}" <<'PY'
import json, sys, glob, os
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", sys.argv[2], "section.json"))):
    d = json.load(open(path))
    print(json.dumps(d.get(sys.argv[3]), indent=1)[:int(sys.argv[4])])
PY
