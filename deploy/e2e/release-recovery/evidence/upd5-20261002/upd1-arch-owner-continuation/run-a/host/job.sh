#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd5-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-owner-continuation /var/tmp/cp-upd1-build/20261001t030434z/upd1-artifacts.json upd5-arch-oc-a 2591
