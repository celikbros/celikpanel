#!/bin/bash
# set9 lab 3 (cell 3, a third time, timed click): the same archive (cur B) installed fresh on a NEW Debian 13 guest; the browser starts the update to cur G
export PYTHONDONTWRITEBYTECODE=1
export SET9_DISK_GATE=<scratchpad>/set9/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set9-run/harness-b/deploy/e2e/release-recovery/run-set9.sh cell upd1-debian13-good "$(cat /var/tmp/cp-set9-run/artifacts-cur.path)" s9-d13c 4795 18795 hand-s9c 7200
