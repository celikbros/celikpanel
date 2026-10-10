#!/bin/bash
# set4b: build the candidate (the commit of the disposable clone src-c1) as set4 built `cur`: baseline labelled
# v0.1.0-alpha.81 = the candidate source with the acceptance licence seam switched on at build time; good, defective,
# startcheck and realstart are built because the artifacts document needs them, and are never installed by this run.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
R=/var/tmp/cp-set4b-run
H=$R/harness-c/deploy/e2e/release-recovery
commit=$(sed -n 's/^candidate=//p' $R/src-c1.txt)
go version
echo "=== build $commit $(date -u +%FT%TZ)"
CELIKPANEL_REPO=$R/src-c1 bash $H/run-upd1.sh build $commit > $R/logs/build-c1.out 2> $R/logs/build-c1.err; rc=$?
echo "rc=$rc $(tail -n 2 $R/logs/build-c1.out | tr '\n' ' ')"
echo "=== end $(date -u +%FT%TZ)"
exit $rc
