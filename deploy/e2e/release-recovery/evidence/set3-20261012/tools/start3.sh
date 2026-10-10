#!/bin/bash
# usage: start3.sh JOB...  -> start the named cell jobs detached
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
for n in "$@"; do tr -d '\r' < $J/job-$n.sh > /var/tmp/cp-set3-run/job-$n.sh; bash $J/bg.sh $n /var/tmp/cp-set3-run/job-$n.sh; done
