#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
bash $J/mkcopy.sh c 2>&1 | cut -c1-130 || exit 1
tr -d '\r' < $J/job-prove2.sh > /var/tmp/cp-set3-run/job-prove2.sh
bash $J/bg.sh prove2 /var/tmp/cp-set3-run/job-prove2.sh
