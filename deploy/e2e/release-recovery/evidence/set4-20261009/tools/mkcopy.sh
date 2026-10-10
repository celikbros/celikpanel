#!/bin/bash
# set4: make run copy NAME from the file list, run the offline suites on it, and the dry runs of every cell.
# usage: mkcopy.sh NAME
J=<scratchpad>/set4
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set4-run; L=$R/logs
bash $J/mkov.sh $1 $(tr -d '\r' < $J/files.txt | tr '\n' ' ') > $L/mkov-$1.txt 2>&1 || { tail -n 3 $L/mkov-$1.txt; exit 1; }
head -n 1 $L/mkov-$1.txt
cd $R/harness-$1
rc=0
for t in test_owner_update_trial test_settings_writes_trial test_request_identity_trial test_recovery_candidate_archive test_lab test_guest_probe; do
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $L/offline-$1-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(grep -E '^Ran ' $L/offline-$1-$t.txt) $(tail -n 1 $L/offline-$1-$t.txt)"; [ $r -eq 0 ] || rc=1
done
CUR=/var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json
A81=/var/tmp/cp-upd1-build/20261009t102025z/upd1-artifacts.json
mkdir -p $R/build
H=$R/harness-$1/deploy/e2e/release-recovery
for c in set4-arch set4-debian13 set4-ubuntu; do
  bash $H/run-set4.sh dry-run $c $CUR dry-$c > $R/build/dry-$1-cur-$c.json 2> $R/build/dry-$1-cur-$c.stderr.txt; echo "dry $c cur rc=$?"
done
for c in upd1-debian13-good upd1-ubuntu-good upd1-arch-defective; do
  bash $H/run-set4.sh dry-run $c $A81 dry-$c > $R/build/dry-$1-a81-$c.json 2> $R/build/dry-$1-a81-$c.stderr.txt; echo "dry $c a81 rc=$?"
done
bash $H/run-set4.sh dry-run upd1-arch-defective $CUR dry-cur-def > $R/build/dry-$1-cur-upd1-arch-defective.json 2> $R/build/dry-$1-cur-upd1-arch-defective.stderr.txt; echo "dry upd1-arch-defective cur rc=$?"
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
exit $rc
