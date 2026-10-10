#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
go version
exec bash /var/tmp/cp-set3-run/harness-u/deploy/e2e/release-recovery/run-upd1.sh build --baseline-ref v0.1.0-alpha.81 cfa329676
