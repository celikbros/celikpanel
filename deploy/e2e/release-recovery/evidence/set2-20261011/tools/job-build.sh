#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
go version
exec bash /var/tmp/cp-set2-run/harness-p/deploy/e2e/release-recovery/run-upd1.sh build faa5ef085
