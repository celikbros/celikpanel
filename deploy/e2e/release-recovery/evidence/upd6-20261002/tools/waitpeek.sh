#!/bin/bash
# usage: waitpeek.sh SECONDS -> returns early when the cell job ends
for i in $(seq 1 $(( $1 / 10 ))); do [ -e /var/tmp/cp-upd6-run/logs/cell-archoc.end ] && break; sleep 10; done
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd6run/peek.sh
