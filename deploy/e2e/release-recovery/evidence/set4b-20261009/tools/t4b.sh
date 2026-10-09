#!/bin/bash
# set4b: the offline suite of the new driver, from a scratch copy of the harness folder (nothing is written to the repository).
export PYTHONDONTWRITEBYTECODE=1
T=/var/tmp/cp-set4b-run/t4b
mkdir -p $T/deploy/e2e
rm -rf $T/deploy/e2e/release-recovery
mkdir -p $T/deploy/e2e/release-recovery $T/internal/services
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery'
for f in *.py; do sed 's/\r$//' "$f" > $T/deploy/e2e/release-recovery/$f; done
cp '/mnt/c/CELIKBROS PROJECTS/celikpanel/internal/services/nginx_php_handoff_test.go' $T/internal/services/
cd $T && python3 -m unittest deploy/e2e/release-recovery/test_set4b_trial.py 2>&1 | tail -n 30
