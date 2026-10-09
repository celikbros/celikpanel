#!/bin/bash
# set3: rid3-debian13 once more after the harness fix H40 (run copy e; no owner action).
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-e/deploy/e2e/release-recovery/run-set2.sh cell rid3-debian13 /var/tmp/cp-upd1-build/20261009t061555z/upd1-artifacts.json rid3-d13-b 4421 18482
