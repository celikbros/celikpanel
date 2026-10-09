#!/bin/bash
# usage: art.sh ARTIFACTS_JSON...  -> one line per role
for a in "$@"; do
python3 - "$a" <<'PY'
import json, sys, os
d = json.load(open(sys.argv[1]))
print(sys.argv[1], "source", d["source_head"][:12], "ref", (d.get("baseline_ref") or {}).get("ref"), (d.get("baseline_ref") or {}).get("patched_files"))
for r in ("baseline", "good", "defective", "startcheck", "realstart"):
    if r in d:
        i = d[r]
        print(" ", r, i["version"], i["sequence"], "commit", i["commit"], "tree", i["tree"][:12], "sha256", i["sha256"][:16], "parent", str(i.get("parent"))[:12], "mtime", int(os.path.getmtime(i["archive"])))
PY
done
grep -c . /var/tmp/cp-upd1-build/20261009t070418z/baseline-ref-proof.txt; grep -c identical /var/tmp/cp-upd1-build/20261009t070418z/baseline-ref-proof.txt; grep -c DIFFERENT /var/tmp/cp-upd1-build/20261009t070418z/baseline-ref-proof.txt; sed -n 1,10p /var/tmp/cp-upd1-build/20261009t070418z/baseline-ref-proof.txt | cut -c1-200
grep -n "already\|reus\|exists" /var/tmp/cp-set3-run/logs/build81b.err | head -n 5 | cut -c1-200
