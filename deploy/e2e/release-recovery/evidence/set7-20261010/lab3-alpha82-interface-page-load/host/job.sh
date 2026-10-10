#!/bin/bash
# set7 lab 3 (cell 5, added by the coordinator): the alpha.82-code archive labelled as the baseline (cur B) installed fresh on
# Debian 13 again; NO update: the browser measures full page loads (cell 5) only.
export PYTHONDONTWRITEBYTECODE=1
export SET7_DISK_GATE=<scratchpad>/set7/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set7-run/harness-b/deploy/e2e/release-recovery/run-set7.sh cell upd1-debian13-good "$(cat /var/tmp/cp-set7-run/artifacts-cur.path)" s7-d13-a82b 4771 18771 hand-a82b 7200
