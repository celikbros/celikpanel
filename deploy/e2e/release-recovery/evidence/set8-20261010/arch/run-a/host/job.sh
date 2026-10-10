#!/bin/bash
# set8: the alpha.82 code (cur B) installed fresh on one new lab, then the owner-edit scenarios (cell set8-arch).
export PYTHONDONTWRITEBYTECODE=1
export SET8_DISK_GATE=<scratchpad>/set8/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set8-run/harness-d/deploy/e2e/release-recovery/run-set8.sh cell set8-arch "$(cat /var/tmp/cp-set8-run/artifacts-cur.path)" s8-arch-a 4853 18853
