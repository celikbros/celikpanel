#!/bin/bash
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
tr -d "" < $J/statusloop.sh > /var/tmp/cp-set3-run/statusloop.sh
setsid nohup bash /var/tmp/cp-set3-run/statusloop.sh > /dev/null 2>&1 < /dev/null &
disown
sleep 2; echo started
