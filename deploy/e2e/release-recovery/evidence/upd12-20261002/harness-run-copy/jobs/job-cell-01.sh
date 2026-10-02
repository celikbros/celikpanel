#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd12-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-good /var/tmp/cp-upd1-build/20261002t140055z/upd1-artifacts.json upd12-d13-good-a 3611
