#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
exec bash /var/tmp/cp-upd12-run/harness/deploy/e2e/release-recovery/run-upd1.sh build 6b6f8a0c
