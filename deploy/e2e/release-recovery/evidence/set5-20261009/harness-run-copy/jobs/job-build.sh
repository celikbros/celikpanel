#!/bin/bash
# set5 build, from run copy b (git archive 67b62cc0f without earlier evidence + the set5 harness files).
#  a81: B = the published tag v0.1.0-alpha.81 (unpatched source, test-licence build), whose dist set3 built and
#       which is reused unchanged, named by its full commit; G, D, S = 67b62cc0f labelled v0.1.0-alpha.82, built fresh.
# The fixture commits (release policy; the two defects) exist only in the builder's disposable clone.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
H=/var/tmp/cp-set5-run/harness-b/deploy/e2e/release-recovery
L=/var/tmp/cp-set5-run/logs
go version
echo "=== a81 $(date -u +%FT%TZ)"
export UPD1_REUSE_DIST_OF_COMMIT=a0beb7263d1f4ca72258f6b306f9111ba4e2a334
bash $H/run-upd1.sh build --baseline-ref v0.1.0-alpha.81 67b62cc0f > $L/build-a81.out 2> $L/build-a81.err; rc=$?
echo "a81 rc=$rc $(tail -n 2 $L/build-a81.out | tr '\n' ' ')"
echo "$(date -u +%FT%TZ) build a81 rc=$rc" >> /var/tmp/cp-set5-run/progress.txt
echo "=== end $(date -u +%FT%TZ)"
exit $rc
