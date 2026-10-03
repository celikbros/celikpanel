#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
exec bash /var/tmp/cp-upd13-run/harness/deploy/e2e/release-recovery/run-upd1.sh build f6cdd5a0
