#!/bin/bash
# usage: stage-batch.sh SPEC... (SHORT:LAB:NODE:CELLDIR:RUN); stages each, then removes its overlay disks only when staged OK
for spec in "$@"; do
  IFS=: read -r short lab node celldir run <<< "$spec"
  out=$(bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd11/stagecell11.sh "$spec"); echo "$out"
  if echo "$out" | grep -q '^STAGED-OK '; then bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd11/rmoverlay11.sh $lab $celldir $run; else echo "NOT REMOVED: $lab"; fi
done
