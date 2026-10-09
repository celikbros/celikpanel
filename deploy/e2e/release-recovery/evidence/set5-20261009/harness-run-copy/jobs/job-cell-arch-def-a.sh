#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export SET5_DISK_GATE=/var/tmp/cp-set5-run/gate.sh
export SET5_AFTER_PREPARE=/var/tmp/cp-set5-run/ramnodes.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set5-run/harness-e/deploy/e2e/release-recovery/run-set5.sh cell upd1-arch-defective /var/tmp/cp-upd1-build/20261009t170058z/upd1-artifacts.json s5-arch-def-a 4661 18661
