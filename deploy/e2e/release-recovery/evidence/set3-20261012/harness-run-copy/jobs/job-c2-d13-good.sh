#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-c/deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-good /var/tmp/cp-upd1-build/20261009t070418z/upd1-artifacts.json u14-d13-good-a 4311 18471
