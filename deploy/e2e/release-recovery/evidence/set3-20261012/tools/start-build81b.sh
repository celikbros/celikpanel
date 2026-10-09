#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
bash $J/mkcopy.sh b 2>&1 | cut -c1-160 || exit 1
tr -d '\r' < $J/job-build81b.sh > /var/tmp/cp-set3-run/job-build81b.sh
bash $J/bg.sh build81b /var/tmp/cp-set3-run/job-build81b.sh
