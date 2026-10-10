#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set4-run/harness-c/deploy/e2e/release-recovery/run-set4.sh cell set4-arch /var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json s4-arch-a 4511 18511
