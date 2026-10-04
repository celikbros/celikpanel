#!/bin/bash
set -eu
R=/var/tmp/cp-upd7-run
echo /var/tmp/cp-upd1-build/20261001t111438z/upd1-artifacts.json > $R/ART
ART=$(cat $R/ART)
B=$(dirname $ART)
cp $B/baseline-ref-proof.txt $R/build/
cat $B/baseline-ref-proof.txt | head -12; echo ...; grep -c identical $B/baseline-ref-proof.txt; grep -E 'update.sh|rollback.sh|get.sh|bootstrap|finalize' $B/baseline-ref-proof.txt; tail -2 $B/baseline-ref-proof.txt
python3 - $ART <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("source_head", d["source_head"], "baseline_ref", json.dumps(d.get("baseline_ref")))
for r in ("baseline", "good", "defective"):
    e = d[r]
    print(r, e["version"], e["sequence"], e["commit"], e["tree"], e["sha256"], e["license_mode"], e.get("parent"))
PY
export PYTHONDONTWRITEBYTECODE=1
W=$R/harness/deploy/e2e/release-recovery/run-upd1.sh
cd $R/harness
set +e
bash $W prove "$ART" > $R/build/prove.json 2> $R/build/prove.stderr.txt; echo $? > $R/build/prove.rc
echo "prove rc=$(cat $R/build/prove.rc)"; tail -3 $R/build/prove.stderr.txt
for c in upd1-debian13-good upd1-debian13-owner-continuation upd1-debian13-defective upd1-arch-good upd1-arch-owner-continuation upd1-arch-defective; do
  bash $W dry-run $c "$ART" upd7-dry > $R/build/dry-run-$c.json 2> $R/build/dry-run-$c.stderr.txt; echo $? > $R/build/dry-run-$c.rc
  python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(sys.argv[2],'native_evidence=',d.get('native_evidence'),d['baseline']['version'],d['candidate']['version'],d['provenance']['baseline'][:60])" $R/build/dry-run-$c.json $c || tail -3 $R/build/dry-run-$c.stderr.txt
done
ls -d /var/tmp/cp-release-drill-upd7-dry 2>/dev/null && echo "WARNING: dry-run created a lab" || echo "no dry-run lab created"
