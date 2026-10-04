#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd9-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-busystart /var/tmp/cp-upd1-build/20261001t174240z/upd1-artifacts.json upd9-ub-busy-a 2921
