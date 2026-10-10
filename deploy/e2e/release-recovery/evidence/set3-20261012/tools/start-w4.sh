#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
bash $J/mkcopy.sh e 2>&1 | cut -c1-120 || exit 1
bash $J/startq.sh W4 c2-d13-mr cell-rid3-archb:rid3-arch-b:arch:rid3-arch:run-b
