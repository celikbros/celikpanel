#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set4b-run/harness-a/deploy/e2e/release-recovery/run-set4b.sh cell set4b-diag-ubuntu /var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json s4b-diag-ub 4711 18711
