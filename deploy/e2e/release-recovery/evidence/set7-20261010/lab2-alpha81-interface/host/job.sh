#!/bin/bash
# set7 lab 2: the published v0.1.0-alpha.81 (test-licence build, the dist set3 built, reused) installed fresh on Debian 13;
# the browser starts the update to a81 G (2a0af8866 labelled v0.1.0-alpha.82 / 82).
export PYTHONDONTWRITEBYTECODE=1
export SET7_DISK_GATE=<scratchpad>/set7/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set7-run/harness-b/deploy/e2e/release-recovery/run-set7.sh cell upd1-debian13-good "$(cat /var/tmp/cp-set7-run/artifacts-a81.path)" s7-d13-a81 4761 18761 hand-a81 10800
