#!/bin/bash
# set3: make run copy NAME from the file list, run the offline suites on it. usage: mkcopy.sh NAME
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
export PYTHONDONTWRITEBYTECODE=1
bash $J/mkov.sh $1 $(tr -d '\r' < $J/files.txt | tr '\n' ' ') > /var/tmp/cp-set3-run/logs/mkov-$1.txt 2>&1 || { tail -n 3 /var/tmp/cp-set3-run/logs/mkov-$1.txt; exit 1; }
head -n 1 /var/tmp/cp-set3-run/logs/mkov-$1.txt
cd /var/tmp/cp-set3-run/harness-$1
L=/var/tmp/cp-set3-run/logs; rc=0
for t in test_settings_writes_trial test_request_identity_trial test_owner_update_trial test_recovery_candidate_archive test_lab test_current_worker_baseline test_worker_fixture_origin test_bound_worker_reboot test_guest_bound_worker test_guest_probe; do
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $L/offline-$1-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(grep -E '^Ran ' $L/offline-$1-$t.txt) $(tail -n 1 $L/offline-$1-$t.txt)"; [ $r -eq 0 ] || rc=1
done
exit $rc
