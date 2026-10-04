#!/bin/bash
# usage: pull.sh CELL [RUN] -> copies the staged extract to the scratchpad for reading
S=/var/tmp/cp-upd4-run/stage/upd4-20261001
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run/pulled
mkdir -p $P
cp $S/$1/${2:-run-a}/side/extract.txt $P/$1-${2:-run-a}-extract.txt && echo ok
