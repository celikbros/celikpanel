#!/bin/bash
f=$(ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*$2*/step.json | tail -1); shift 2; python3 /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd10/stepview.py $f "$@"
