#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd11-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-mgmt-off-reboot /var/tmp/cp-upd1-build/20261001t221215z/upd1-artifacts.json upd11-arch-mr-a 3311
