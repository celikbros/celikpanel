#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
exec bash /var/tmp/cp-upd11-run/harness/deploy/e2e/release-recovery/run-upd1.sh build --baseline-ref v0.1.0-alpha.80 48e54657
