#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd3-run/harness-h8/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-good /var/tmp/cp-upd1-build/20260930t174023z/upd1-artifacts.json upd3-arch-good-a 2461
