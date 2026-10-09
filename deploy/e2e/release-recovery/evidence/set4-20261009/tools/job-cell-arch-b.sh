#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set4-run/harness-f/deploy/e2e/release-recovery/run-set4.sh cell set4-arch /var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json s4-arch-b 4611 18611
