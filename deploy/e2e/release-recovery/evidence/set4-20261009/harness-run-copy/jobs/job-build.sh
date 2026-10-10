#!/bin/bash
# set4 builds, one after the other, from run copy b (git archive 557b554eb + the builder's set4 change).
#  cur: B = the source 557b554eb labelled v0.1.0-alpha.81 (the installed candidate of the fresh-install cells), G, D, S, R.
#  a81: B = the published tag v0.1.0-alpha.81, whose dist an earlier run (set3) built and which is reused unchanged
#       (named by its full commit); G, D, S = 557b554eb labelled v0.1.0-alpha.82, built fresh.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set4-run/harness-b/deploy/e2e/release-recovery
go version
echo "=== cur $(date -u +%FT%TZ)"
bash $H/run-upd1.sh build 557b554eb > /var/tmp/cp-set4-run/logs/build-cur.out 2> /var/tmp/cp-set4-run/logs/build-cur.err; rc1=$?
echo "cur rc=$rc1 $(tail -n 2 /var/tmp/cp-set4-run/logs/build-cur.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build cur rc=$rc1" >> /var/tmp/cp-set4-run/progress.txt
echo "=== a81 $(date -u +%FT%TZ)"
export UPD1_REUSE_DIST_OF_COMMIT=a0beb7263d1f4ca72258f6b306f9111ba4e2a334
bash $H/run-upd1.sh build --baseline-ref v0.1.0-alpha.81 557b554eb > /var/tmp/cp-set4-run/logs/build-a81.out 2> /var/tmp/cp-set4-run/logs/build-a81.err; rc2=$?
echo "a81 rc=$rc2 $(tail -n 2 /var/tmp/cp-set4-run/logs/build-a81.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build a81 rc=$rc2" >> /var/tmp/cp-set4-run/progress.txt
echo "=== end $(date -u +%FT%TZ)"
[ $rc1 -eq 0 ] && [ $rc2 -eq 0 ]
