#!/bin/bash
# set7 builds, from run copy a (git archive 2a0af8866 = v0.1.0-alpha.82, without earlier evidence; no harness file of the working tree).
#  cur: `run-upd1.sh build 2a0af8866`: B = 2a0af8866 labelled v0.1.0-alpha.81 / 81 with the acceptance-test licence build tag
#       (cell 1 installs it fresh: the alpha.82 interface); G = B + release policy v0.1.0-alpha.82 / 82 (cell 1's update target).
#       D, S, R are built because the artifacts document needs them and are never installed.
#  a81: `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 2a0af8866`: B = the published tag v0.1.0-alpha.81 (unpatched source,
#       test-licence build; the dist set3 built, reused unchanged, named by its full commit); G = 2a0af8866 labelled
#       v0.1.0-alpha.82 / 82 (cell 2's update target). D, S built because the document needs them, never installed.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set7-run/harness-a/deploy/e2e/release-recovery
L=/var/tmp/cp-set7-run/logs
go version
rcall=0
echo "=== cur $(date -u +%FT%TZ)"
bash $H/run-upd1.sh build 2a0af8866 > $L/build-cur.out 2> $L/build-cur.err; rc=$?
echo "cur rc=$rc $(tail -n 2 $L/build-cur.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build cur rc=$rc" >> /var/tmp/cp-set7-run/progress.txt
[ $rc -eq 0 ] || rcall=1
sleep 2
echo "=== a81 $(date -u +%FT%TZ)"
export UPD1_REUSE_DIST_OF_COMMIT=a0beb7263d1f4ca72258f6b306f9111ba4e2a334
bash $H/run-upd1.sh build --baseline-ref v0.1.0-alpha.81 2a0af8866 > $L/build-a81.out 2> $L/build-a81.err; rc=$?
echo "a81 rc=$rc $(tail -n 2 $L/build-a81.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build a81 rc=$rc" >> /var/tmp/cp-set7-run/progress.txt
[ $rc -eq 0 ] || rcall=1
echo "=== end $(date -u +%FT%TZ)"
exit $rcall
