#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set2-run/harness-c/deploy/e2e/release-recovery/run-set2.sh cell rid-debian13 /var/tmp/cp-upd1-build/20261009t030455z/upd1-artifacts.json rid-d13-a 4131 18454
