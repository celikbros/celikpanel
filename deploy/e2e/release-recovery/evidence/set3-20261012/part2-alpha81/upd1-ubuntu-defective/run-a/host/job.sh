#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-c/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-defective /var/tmp/cp-upd1-build/20261009t070418z/upd1-artifacts.json u14-ub-def-a 4371 18477
