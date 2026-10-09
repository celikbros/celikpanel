#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set4b-run/harness-c/deploy/e2e/release-recovery/run-set4b.sh cell set4b-debian13 /var/tmp/cp-upd1-build/20261009t143202z/upd1-artifacts.json s4b-d13-a 4722 18722
