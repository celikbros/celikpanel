#!/bin/bash
R=/var/tmp/cp-set2-run
python3 - $(cat $R/art-cur.txt) <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("source_head", d["source_head"])
for r in ("baseline", "good", "defective", "startcheck", "realstart"):
    if r in d:
        e = d[r]
        print(r, e["version"], e["sequence"], e["commit"], e["tree"], e["sha256"], e["license_mode"], e.get("parent"))
PY
git -c safe.directory='*' -C "/mnt/c/CELIKBROS PROJECTS/celikpanel" rev-parse 'faa5ef085^{tree}'
