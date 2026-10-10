#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-c/deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-defective /var/tmp/cp-upd1-build/20261009t070418z/upd1-artifacts.json u14-d13-def-a 4321 18472
