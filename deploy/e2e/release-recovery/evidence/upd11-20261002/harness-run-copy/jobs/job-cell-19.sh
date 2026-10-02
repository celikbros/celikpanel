#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd11-run/harness-h21/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-good /var/tmp/cp-upd1-build/20261001t221533z/upd1-artifacts.json upd11-a80-ub-good-a 3391
