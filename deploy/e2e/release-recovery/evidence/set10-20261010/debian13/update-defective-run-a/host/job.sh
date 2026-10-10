#!/bin/bash
# set10 part B: the published alpha.81 installed, sites prepared on it, then the owner's update to cd46ca595 (G, good)
# or to the defective candidate D (automatic return) (cell upd1-debian13-defective).
export PYTHONDONTWRITEBYTECODE=1
export SET10_NEIGHBOUR_GATE=<scratchpad>/set10/tools/neighbour.sh
export SET10_DISK_GATE=<scratchpad>/set10/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set10-run/harness-g/deploy/e2e/release-recovery/run-set10.sh cell upd1-debian13-defective "$(cat /var/tmp/cp-set10-run/artifacts-a81.path)" s10-d13d-${SET10_RUN:-a} 4865 18865
