#!/bin/bash
set -eu
R=/var/tmp/cp-upd5-run
echo /var/tmp/cp-upd1-build/20261001t030434z/upd1-artifacts.json > $R/ART
python3 - $(cat $R/ART) <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("source_head", d["source_head"])
for r in ("baseline", "good", "defective", "startcheck", "realstart"):
    e = d[r]
    print(r, e["version"], e["sequence"], e["commit"], e["tree"], e["sha256"], e["license_mode"], e.get("parent"))
PY
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd5run/mkjobs.sh
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd5run/prove-dry.sh
