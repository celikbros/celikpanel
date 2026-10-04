#!/bin/bash
L=/var/tmp/cp-upd13-run/logs
for t in cur a80; do
  date -u +%FT%TZ > $L/build-$t.start
  bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13/job-build-$t.sh > $L/build-$t.out 2> $L/build-$t.err; echo $? > $L/build-$t.rc
  date -u +%FT%TZ > $L/build-$t.end
  echo "build-$t rc=$(cat $L/build-$t.rc)"
done
