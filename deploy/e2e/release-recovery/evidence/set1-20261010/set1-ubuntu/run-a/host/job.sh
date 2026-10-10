#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set1-run/harness-set1b/deploy/e2e/release-recovery/run-set1.sh cell set1-ubuntu /var/tmp/cp-upd1-build/20261008t210046z/upd1-artifacts.json set1-ub-a 4021
