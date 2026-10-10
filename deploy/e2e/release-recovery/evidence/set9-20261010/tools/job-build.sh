#!/bin/bash
# set9 build, from run copy a (git archive 7c3a05809 without earlier evidence; no harness file of the working tree).
#  cur: `run-upd1.sh build 7c3a05809`: B = 7c3a05809 labelled v0.1.0-alpha.81 / 81 with the acceptance-test licence build
#       tag (installed fresh: the interface of e508af230); G = B + release policy v0.1.0-alpha.82 / 82 (cell 3's update
#       target, the same interface). D, S, R are built because the artifacts document needs them and are never installed.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set9-run/harness-a/deploy/e2e/release-recovery
L=/var/tmp/cp-set9-run/logs
go version
echo "=== cur $(date -u +%FT%TZ)"
bash $H/run-upd1.sh build 7c3a05809 > $L/build-cur.out 2> $L/build-cur.err; rc=$?
echo "cur rc=$rc $(tail -n 2 $L/build-cur.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build cur rc=$rc" >> /var/tmp/cp-set9-run/progress.txt
echo "=== end $(date -u +%FT%TZ)"
exit $rc
