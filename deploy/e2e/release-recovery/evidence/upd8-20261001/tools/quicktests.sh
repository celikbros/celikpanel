#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
D=/var/tmp/cp-upd8-quick
rm -rf $D; mkdir -p $D
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
# copy the working-tree e2e dir (CRLF normalized) so tests never write into the repository
cp -r "$REPO/deploy/e2e" $D/e2e
find $D/e2e -name '*.py' -o -name '*.sh' -o -name '*.json' | while read f; do sed -i 's/\r$//' "$f"; done
cd $D/e2e/release-recovery
for t in test_lab test_owner_update_trial test_recovery_candidate_archive test_current_worker_baseline test_worker_fixture_origin test_bound_worker_reboot test_guest_bound_worker; do
  python3 -m unittest $t.py > $D/$t.txt 2>&1; echo "$t rc=$? $(tail -n 1 $D/$t.txt)"
done
