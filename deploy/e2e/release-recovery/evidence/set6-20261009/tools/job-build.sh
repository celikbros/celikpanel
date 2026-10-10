#!/bin/bash
# set6 builds, from run copy a (git archive 72b879eea without earlier evidence; no harness file of the working tree).
#  cur: `run-upd1.sh build 72b879eea`: B = 72b879eea labelled v0.1.0-alpha.81 / 81 with the acceptance-test licence
#       build tag (the archive the three fresh-install cells install); G, D, S, R are built because the artifacts
#       document needs them and are never installed.
#  a81: `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 72b879eea`: B = the published tag v0.1.0-alpha.81
#       (unpatched source, test-licence build), whose dist set3 built and which is reused unchanged, named by its
#       full commit; G, D, S = 72b879eea labelled v0.1.0-alpha.82 / 82, built fresh (only G is installed, by the update).
# The fixture commits (release policy; the defects) exist only in the builder's disposable clones.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set6-run/harness-a/deploy/e2e/release-recovery
L=/var/tmp/cp-set6-run/logs
go version
rcall=0
echo "=== cur $(date -u +%FT%TZ)"
bash $H/run-upd1.sh build 72b879eea > $L/build-cur.out 2> $L/build-cur.err; rc=$?
echo "cur rc=$rc $(tail -n 2 $L/build-cur.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build cur rc=$rc" >> /var/tmp/cp-set6-run/progress.txt
[ $rc -eq 0 ] || rcall=1
sleep 2
echo "=== a81 $(date -u +%FT%TZ)"
export UPD1_REUSE_DIST_OF_COMMIT=a0beb7263d1f4ca72258f6b306f9111ba4e2a334
bash $H/run-upd1.sh build --baseline-ref v0.1.0-alpha.81 72b879eea > $L/build-a81.out 2> $L/build-a81.err; rc=$?
echo "a81 rc=$rc $(tail -n 2 $L/build-a81.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build a81 rc=$rc" >> /var/tmp/cp-set6-run/progress.txt
[ $rc -eq 0 ] || rcall=1
echo "=== end $(date -u +%FT%TZ)"
exit $rcall
