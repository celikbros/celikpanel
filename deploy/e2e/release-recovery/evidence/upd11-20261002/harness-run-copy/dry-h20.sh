#!/bin/bash
cd /var/tmp/cp-upd11-run/harness-h20
export PYTHONDONTWRITEBYTECODE=1
A=/var/tmp/cp-upd1-build/20261001t221215z/upd1-artifacts.json
for c in upd1-arch-mgmt-off-reboot upd1-ubuntu-mgmt-off-reboot upd1-ubuntu-startcheck; do
  bash deploy/e2e/release-recovery/run-upd1.sh dry-run $c $A upd11-dry > /var/tmp/cp-upd11-run/build/h20-dry-run-$c.json 2> /var/tmp/cp-upd11-run/build/h20-dry-run-$c.stderr.txt; echo "dry $c rc=$?"
done
ls -d /var/tmp/cp-release-drill-upd11-dry 2>/dev/null && echo "WARNING lab created" || echo "no dry-run lab created"
