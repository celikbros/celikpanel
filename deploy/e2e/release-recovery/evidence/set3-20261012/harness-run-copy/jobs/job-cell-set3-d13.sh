#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-d/deploy/e2e/release-recovery/run-set2.sh cell set3-debian13 /var/tmp/cp-upd1-build/20261009t061555z/upd1-artifacts.json set3-d13-a 4211 18461
