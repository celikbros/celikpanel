#!/bin/bash
# usage: reext.sh LAB NODE CELL [RUN] -> re-run the extraction of an already staged cell and verify its driver SHA256SUMS
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd5run
S=/var/tmp/cp-upd5-run/stage/upd5-20261002
D=$S/$3/${4:-run-a}
( cd $D && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 && echo "$3 driver SHA256SUMS ok" || echo "$3 driver SHA256SUMS FAILED" )
python3 $P/ext5.py $1 $2 $D/side/extract.txt
mkdir -p $P/pulled; cp $D/side/extract.txt $P/pulled/$3-${4:-run-a}-extract.txt
