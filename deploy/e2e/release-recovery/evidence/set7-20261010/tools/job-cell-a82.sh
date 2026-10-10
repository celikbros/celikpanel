#!/bin/bash
# set7 lab 1: the alpha.82-code archive labelled as the baseline (cur B) installed fresh on Debian 13; the browser
# starts the update to cur G (alpha.82 code labelled v0.1.0-alpha.82 / 82); then the hidden-tab cells on the same guest.
export PYTHONDONTWRITEBYTECODE=1
export SET7_DISK_GATE=<scratchpad>/set7/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set7-run/harness-b/deploy/e2e/release-recovery/run-set7.sh cell upd1-debian13-good "$(cat /var/tmp/cp-set7-run/artifacts-cur.path)" s7-d13-a82 4751 18751 hand-a82 14400
