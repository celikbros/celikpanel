#!/bin/bash
# set3: proof of the rebuilt published-baseline document (with the start-check candidate) and its dry runs, on copy c.
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set3-run; H=$R/harness-c/deploy/e2e/release-recovery
ART=/var/tmp/cp-upd1-build/20261009t070418z/upd1-artifacts.json
bash $H/run-upd1.sh prove "$ART" > $R/build/a81-prove.json 2> $R/build/a81-prove.stderr.txt; rc=$?
echo "prove a81 (rebuilt) rc=$rc"
rm -f $R/build/a81-dry-*.json $R/build/a81-dry-*.stderr.txt
for c in upd1-debian13-good upd1-debian13-defective upd1-debian13-startcheck upd1-debian13-owner-continuation upd1-debian13-mgmt-off-reboot upd1-ubuntu-good upd1-ubuntu-defective upd1-ubuntu-owner-continuation upd1-arch-good upd1-arch-defective; do
  bash $H/run-upd1.sh dry-run $c "$ART" dry-$c > $R/build/a81-dry-$c.json 2> $R/build/a81-dry-$c.stderr.txt; r=$?
  echo "dry a81 $c rc=$r native_evidence=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("native_evidence"))' $R/build/a81-dry-$c.json 2>&1 | tail -n 1 | cut -c1-80)"; [ $r -eq 0 ] || rc=1
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null && echo "A DRY RUN CREATED A LAB" || echo "no dry-run lab exists"
exit $rc
