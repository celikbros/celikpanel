#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd10-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-setuponce /var/tmp/cp-upd1-build/20261001t210302z/upd1-artifacts.json upd10-ub-once-a 3111
