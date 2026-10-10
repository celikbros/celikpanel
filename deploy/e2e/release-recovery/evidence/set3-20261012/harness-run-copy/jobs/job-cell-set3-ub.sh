#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set3-run/harness-a/deploy/e2e/release-recovery/run-set2.sh cell set3-ubuntu /var/tmp/cp-upd1-build/20261009t061555z/upd1-artifacts.json set3-ub-a 4221 18462
