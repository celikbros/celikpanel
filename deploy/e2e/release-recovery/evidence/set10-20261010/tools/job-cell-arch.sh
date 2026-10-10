#!/bin/bash
# set10 part A: cd46ca595 (fresh B) installed fresh on one new lab, then the D-031 sections (cell set10-arch).
export PYTHONDONTWRITEBYTECODE=1
export SET10_NEIGHBOUR_GATE=<scratchpad>/set10/tools/neighbour.sh
export SET10_DISK_GATE=<scratchpad>/set10/tools/gate.sh
export CELIKPANEL_LAB_LINK_BASE_IMAGES=1
exec bash /var/tmp/cp-set10-run/harness-e/deploy/e2e/release-recovery/run-set10.sh cell set10-arch "$(cat /var/tmp/cp-set10-run/artifacts-fresh.path)" s10-arch-${SET10_RUN:-a} 4863 18863
