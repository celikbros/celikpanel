#!/bin/bash
# set9 lab 2 (cell 3 again): the same archive (cur B) installed fresh on a NEW Debian 13 guest; the browser starts the update to cur G
export PYTHONDONTWRITEBYTECODE=1
export SET9_DISK_GATE=<scratchpad>/set9/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set9-run/harness-b/deploy/e2e/release-recovery/run-set9.sh cell upd1-debian13-good "$(cat /var/tmp/cp-set9-run/artifacts-cur.path)" s9-d13b 4793 18793 hand-s9b 7200
