#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
cd /var/tmp/cp-upd7-run/harness
L=/var/tmp/cp-upd7-run/logs
rc=0
for t in test_owner_update_trial test_recovery_candidate_archive test_current_worker_baseline test_worker_fixture_origin test_bound_worker_reboot test_guest_bound_worker; do
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $L/offline-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(tail -n 1 $L/offline-$t.txt)"; [ $r -eq 0 ] || rc=1
done
exit $rc
