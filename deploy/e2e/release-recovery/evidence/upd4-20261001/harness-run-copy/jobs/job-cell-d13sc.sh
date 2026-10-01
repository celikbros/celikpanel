#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd4-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-debian13-startcheck /var/tmp/cp-upd1-build/20260930t221920z/upd1-artifacts.json upd4-d13-sc-a 2401
