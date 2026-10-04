#!/bin/bash
# upd10: the unmodified c67d1861 harness test (does the efcba145-era probe assertion still hold against c855a757?).
set -u
P=/var/tmp/cp-upd10-run/pristine2
mkdir -p $P
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' archive c67d1861 | tar -x -C $P
cd $P && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py > /var/tmp/cp-upd10-run/logs/offline-pristine-test_owner_update_trial.txt 2>&1; echo "rc=$?"
grep -E '^(FAIL|ERROR):|^Ran|^FAILED|^OK|AssertionError' /var/tmp/cp-upd10-run/logs/offline-pristine-test_owner_update_trial.txt | head
