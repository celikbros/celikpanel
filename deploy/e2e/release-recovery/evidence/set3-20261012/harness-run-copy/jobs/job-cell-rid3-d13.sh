#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-a/deploy/e2e/release-recovery/run-set2.sh cell rid3-debian13 /var/tmp/cp-upd1-build/20261009t061555z/upd1-artifacts.json rid3-d13-a 4231 18463
