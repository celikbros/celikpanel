#!/bin/bash
# set8 build, from run copy a (git archive 2a0af8866 = v0.1.0-alpha.82, without earlier evidence; no harness file of the
# working tree): `run-upd1.sh build 2a0af8866`: B = 2a0af8866 labelled v0.1.0-alpha.81 / 81 with the acceptance-test licence
# build tag (installed fresh by every cell: the alpha.82 code). G, D, S, R are built because the artifacts document needs
# them and are never installed.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set8-run/harness-a/deploy/e2e/release-recovery
L=/var/tmp/cp-set8-run/logs
go version
echo "=== cur $(date -u +%FT%TZ)"
bash $H/run-upd1.sh build 2a0af8866 > $L/build-cur.out 2> $L/build-cur.err; rc=$?
echo "cur rc=$rc $(tail -n 2 $L/build-cur.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build cur rc=$rc" >> /var/tmp/cp-set8-run/progress.txt
echo "=== end $(date -u +%FT%TZ)"
exit $rc
