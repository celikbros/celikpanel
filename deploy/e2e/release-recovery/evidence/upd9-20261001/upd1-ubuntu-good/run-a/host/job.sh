#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd9-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-good /var/tmp/cp-upd1-build/20261001t174610z/upd1-artifacts.json upd9-ub-a80-a 2931
