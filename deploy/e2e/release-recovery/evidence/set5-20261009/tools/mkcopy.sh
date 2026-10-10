#!/bin/bash
# set5: make run copy NAME from the file list and run the offline suites on it. usage: mkcopy.sh NAME
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set5-run; L=$R/logs
mkdir -p $L
[ -e $R/progress.txt ] || echo "$(date -u +%FT%TZ) set5 run directory created" > $R/progress.txt
bash $J/mkov.sh $1 $(tr -d '\r' < $J/files.txt | tr '\n' ' ') > $L/mkov-$1.txt 2>&1 || { tail -n 5 $L/mkov-$1.txt; exit 1; }
head -n 6 $L/mkov-$1.txt
cd $R/harness-$1
rc=0
for t in test_set5_trial test_owner_update_trial test_settings_writes_trial test_request_identity_trial test_recovery_candidate_archive test_lab test_guest_probe test_set4b_trial; do
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $L/offline-$1-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(grep -E '^Ran ' $L/offline-$1-$t.txt) $(tail -n 1 $L/offline-$1-$t.txt)"; [ $r -eq 0 ] || rc=1
done
du -sh $R/harness-$1 | cut -f1
exit $rc
