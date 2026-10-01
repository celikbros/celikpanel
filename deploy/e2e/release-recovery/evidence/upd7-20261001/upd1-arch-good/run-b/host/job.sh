#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd7-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-good /var/tmp/cp-upd1-build/20261001t111438z/upd1-artifacts.json upd7-arch-good-b 2751
