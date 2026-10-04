#!/bin/bash
# prove + dry-run for all twelve cells (no guest, no lab). Output under /var/tmp/cp-upd4-run/build.
set -u
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-upd4-run
W=$R/harness/deploy/e2e/release-recovery/run-upd1.sh
ART=$(cat $R/ART)
cd $R/harness
bash $W prove "$ART" > $R/build/prove.json 2> $R/build/prove.stderr.txt
echo $? > $R/build/prove.rc
echo "prove rc=$(cat $R/build/prove.rc)"
for c in upd1-debian13-good upd1-debian13-defective upd1-debian13-startcheck upd1-debian13-realstart \
         upd1-debian13-owner-continuation upd1-debian13-mgmt-off-reboot \
         upd1-arch-good upd1-arch-defective upd1-arch-startcheck upd1-arch-realstart \
         upd1-arch-owner-continuation upd1-arch-mgmt-off-reboot; do
  bash $W dry-run $c "$ART" upd4-dry > $R/build/dry-run-$c.json 2> $R/build/dry-run-$c.stderr.txt
  rc=$?
  ne=$(python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d.get('native_evidence'), sorted(d)[:12])" $R/build/dry-run-$c.json 2>&1 | head -c 300)
  echo "dry-run $c rc=$rc $ne"
done
ls -d /var/tmp/cp-release-drill-upd4-dry 2>/dev/null && echo "WARNING: dry-run created a lab" || echo "no dry-run lab created"
