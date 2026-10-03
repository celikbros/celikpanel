#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd13-run/harness-h22/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-good /var/tmp/cp-upd1-build/20261002t203514z/upd1-artifacts.json upd13-a80-ub-good-a 3901
