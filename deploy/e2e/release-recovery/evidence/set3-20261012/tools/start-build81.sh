#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
[ -e /var/tmp/cp-set3-run/logs/build.end ] || { echo "build 1 still running"; exit 1; }
bash $J/mkov.sh u owner_update_trial.py build-upd1-artifacts.sh 2>&1 | tail -n 4
bash $J/bg.sh build81 $J/job-build81.sh
