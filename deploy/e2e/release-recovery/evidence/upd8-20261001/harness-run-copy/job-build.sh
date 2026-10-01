#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd8-run/harness/deploy/e2e/release-recovery/run-upd1.sh build --baseline-ref v0.1.0-alpha.80 e9e2d3f3
