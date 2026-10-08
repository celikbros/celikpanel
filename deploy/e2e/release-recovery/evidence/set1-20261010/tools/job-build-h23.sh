#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
exec bash /var/tmp/cp-set1-run/harness-h23/deploy/e2e/release-recovery/run-upd1.sh build c4cf7fd9
