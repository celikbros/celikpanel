#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
bash $J/mkcopy.sh d 2>&1 | cut -c1-120 || exit 1
P=part2-alpha81
bash $J/startq.sh W1 cell-rid3-d13 cell-set3-d13:set3-d13-a:debian13:set3-debian13:run-a c2-d13-good:u14-d13-good-a:debian13:upd1-debian13-good:run-a:$P c2-d13-def:u14-d13-def-a:debian13:upd1-debian13-defective:run-a:$P c2-d13-sc:u14-d13-sc-a:debian13:upd1-debian13-startcheck:run-a:$P c2-d13-oc:u14-d13-oc-a:debian13:upd1-debian13-owner-continuation:run-a:$P
bash $J/startq.sh W2 cell-set3-ub cell-rid3-arch:rid3-arch-a:arch:rid3-arch:run-a c2-arch-good:u14-arch-good-a:arch:upd1-arch-good:run-a:$P c2-arch-def:u14-arch-def-a:arch:upd1-arch-defective:run-a:$P c2-d13-mr:u14-d13-mr-a:debian13:upd1-debian13-mgmt-off-reboot:run-a:$P
cat /var/tmp/cp-set3-run/logs/queue.log
