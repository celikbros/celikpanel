#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-set4-run/harness-d/deploy/e2e/release-recovery/run-set4.sh cell upd1-arch-defective /var/tmp/cp-upd1-build/20261009t102025z/upd1-artifacts.json s4-u12-arch-a 4561 18561
