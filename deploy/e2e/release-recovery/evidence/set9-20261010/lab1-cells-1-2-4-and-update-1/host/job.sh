#!/bin/bash
# set9: the alpha.83-candidate archive (7c3a05809 labelled as the baseline, cur B) installed fresh on Debian 13; the browser
# measures cold loads, a slow session read, known negatives and a stopped Panel, then starts the update to cur G from Settings.
export PYTHONDONTWRITEBYTECODE=1
export SET9_DISK_GATE=<scratchpad>/set9/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set9-run/harness-b/deploy/e2e/release-recovery/run-set9.sh cell upd1-debian13-good "$(cat /var/tmp/cp-set9-run/artifacts-cur.path)" s9-d13 4791 18791 hand-s9 14400
