#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
export SET6_DISK_GATE=/var/tmp/cp-set6-run/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set6-run/harness-c/deploy/e2e/release-recovery/run-set6.sh cell upd1-arch-good /var/tmp/cp-upd1-build/20261009t213912z/upd1-artifacts.json s6-arch-good-a 4641 18641
