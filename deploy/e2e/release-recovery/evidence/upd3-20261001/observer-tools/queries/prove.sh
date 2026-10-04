#!/bin/bash
R=/var/tmp/cp-upd3-run
ART=/var/tmp/cp-upd1-build/20260930t174023z/upd1-artifacts.json
echo $ART > $R/ART
mkdir -p $R/build
cd $R/harness
export PYTHONDONTWRITEBYTECODE=1
W=deploy/e2e/release-recovery/run-upd1.sh
bash $W prove $ART > $R/build/prove.json 2> $R/build/prove.stderr.txt; echo $? > $R/build/prove.rc; echo "prove rc=$(cat $R/build/prove.rc)"
for c in upd1-debian13-good upd1-debian13-defective upd1-debian13-startcheck upd1-debian13-realstart upd1-arch-good upd1-arch-defective upd1-arch-startcheck upd1-arch-realstart; do
  bash $W dry-run $c $ART upd3-dry > $R/build/dry-run-$c.json 2> $R/build/dry-run-$c.stderr.txt; rc=$?
  echo "$c rc=$rc native_evidence=$(python3 -c "import json,sys;d=json.load(open('$R/build/dry-run-$c.json'));print(d.get('native_evidence'), d.get('verdict') or d.get('status') or list(d)[:6])" 2>&1)"
done
ls -d /var/tmp/cp-release-drill-upd3-dry 2>/dev/null && echo "DRY LAB EXISTS" || echo "no dry lab created"
python3 - $ART <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
print("source_head", d["source_head"]); print("clone", d["clone"])
for r in ("baseline","good","defective","startcheck","realstart"):
    e=d[r]; print(r, e["version"], e["sequence"], e["commit"], e["tree"], e["sha256"], e.get("parent"), e["license_mode"], e["product_web_src"])
PY
python3 -c "import json;d=json.load(open('$R/build/prove.json'));print(json.dumps(d)[:1500])"
