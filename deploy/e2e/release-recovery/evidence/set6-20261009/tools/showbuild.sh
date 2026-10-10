#!/bin/bash
# set6 read-only: the two artifacts documents in short
for f in $(cat /var/tmp/cp-set6-run/logs/build-cur.out /var/tmp/cp-set6-run/logs/build-a81.out | grep upd1-artifacts.json); do
  echo "== $f"
  python3 -I - "$f" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("source_head", d["source_head"], "| baseline_ref", (d.get("baseline_ref") or {}).get("ref"), "patched", (d.get("baseline_ref") or {}).get("patched_files"))
for role in ("baseline", "good", "defective", "startcheck", "realstart"):
    if role in d:
        i = d[role]
        print(role, i["version"], i["sequence"], "commit", i["commit"], "tree", i["tree"], "sha256", i["sha256"], i.get("license_mode"), "REUSED" if i.get("reused_dist") else "")
PY
done
grep -c BUILD-UPD1-REUSED /var/tmp/cp-set6-run/logs/build-a81.err
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse '72b879eea^{tree}'
