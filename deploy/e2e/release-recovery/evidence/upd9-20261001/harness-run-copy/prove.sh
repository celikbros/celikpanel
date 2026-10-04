#!/bin/bash
# usage: prove.sh TAG ARTIFACTS CELL...   -> prove + dry-runs, outputs under /var/tmp/cp-upd9-run/build/TAG-*
set -u
R=/var/tmp/cp-upd9-run
tag=$1; ART=$2; shift 2
export PYTHONDONTWRITEBYTECODE=1
W=$R/harness/deploy/e2e/release-recovery/run-upd1.sh
cd $R/harness
python3 - $ART <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("source_head", d["source_head"], "baseline_ref", json.dumps(d.get("baseline_ref")))
for r in ("baseline", "good", "defective", "startcheck", "realstart"):
    if r in d:
        e = d[r]
        print(r, e["version"], e["sequence"], e["commit"], e["tree"], e["sha256"], e["license_mode"], e.get("parent"))
PY
bash $W prove "$ART" > $R/build/$tag-prove.json 2> $R/build/$tag-prove.stderr.txt; echo "prove rc=$?"
for c in "$@"; do
  bash $W dry-run $c "$ART" upd9-dry > $R/build/$tag-dry-run-$c.json 2> $R/build/$tag-dry-run-$c.stderr.txt; echo "dry $c rc=$?"
  python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(sys.argv[2],'native_evidence=',d.get('native_evidence'),d['baseline']['version'],d['candidate']['version'],[s['name'] for s in d['steps']]);print(' expected:',d['expected_outcome'][:160])" $R/build/$tag-dry-run-$c.json $c || tail -3 $R/build/$tag-dry-run-$c.stderr.txt
done
ls -d /var/tmp/cp-release-drill-upd9-dry 2>/dev/null && echo "WARNING: dry-run created a lab" || echo "no dry-run lab created"
