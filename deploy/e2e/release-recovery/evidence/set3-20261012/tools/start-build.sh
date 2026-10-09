#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
bash $J/mk-pristine.sh 2>&1 | tail -n 5
bash $J/bg.sh build $J/job-build.sh
