#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set2-run/harness-a/deploy/e2e/release-recovery/run-set2.sh cell set2-debian13 /var/tmp/cp-upd1-build/20261009t030455z/upd1-artifacts.json set2-d13-a 4111 18453
