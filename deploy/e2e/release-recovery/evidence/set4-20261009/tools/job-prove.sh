#!/bin/bash
# set4: read-only host proof of every archive of both builds (inventory, release policy, committed source).
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set4-run/harness-b/deploy/e2e/release-recovery
R=/var/tmp/cp-set4-run
mkdir -p $R/build
bash $H/run-upd1.sh prove /var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json > $R/build/cur-prove.json 2> $R/build/cur-prove.stderr.txt; rc1=$?
echo "prove cur rc=$rc1"
bash $H/run-upd1.sh prove /var/tmp/cp-upd1-build/20261009t102025z/upd1-artifacts.json > $R/build/a81-prove.json 2> $R/build/a81-prove.stderr.txt; rc2=$?
echo "prove a81 rc=$rc2"
[ $rc1 -eq 0 ] && [ $rc2 -eq 0 ]
