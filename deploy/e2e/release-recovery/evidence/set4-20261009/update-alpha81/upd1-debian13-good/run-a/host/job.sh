#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set4-run/harness-c/deploy/e2e/release-recovery/run-set4.sh cell upd1-debian13-good /var/tmp/cp-upd1-build/20261009t102025z/upd1-artifacts.json s4-u9-d13-a 4541 18541
