#!/bin/bash
# set1: the offline suites on one run copy. usage: HARNESS_DIR=harness-set1 job-offline.sh TAG
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
cd /var/tmp/cp-set1-run/${HARNESS_DIR:-harness}
L=/var/tmp/cp-set1-run/logs
tag=${1:-x}
rc=0
for t in test_settings_writes_trial test_owner_update_trial test_recovery_candidate_archive test_lab test_current_worker_baseline test_worker_fixture_origin test_bound_worker_reboot test_guest_bound_worker test_guest_probe; do
  [ -f deploy/e2e/release-recovery/$t.py ] || { echo "$t absent in this copy"; continue; }
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $L/offline-$tag-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(tail -n 1 $L/offline-$tag-$t.txt) $(grep -E '^Ran ' $L/offline-$tag-$t.txt)"; [ $r -eq 0 ] || rc=1
done
exit $rc
