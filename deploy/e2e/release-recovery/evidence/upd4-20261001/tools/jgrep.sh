#!/bin/bash
# usage: jgrep.sh CELL [FILEGLOB] -> grep (pattern from pat.txt next to this script) in a staged cell's files
S=/var/tmp/cp-upd4-run/stage/upd4-20261001
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
cell=$1; g=${2:-steps/*-collect/journal-*.txt}
pat=$(tr -d '\r\n' < $P/pat.txt)
cd $S/$cell/${RUN:-run-a} || exit 1
wc -l $g | tail -n 20
grep -nEi -- "$pat" $g | cut -c1-${WIDTH:-4000} | head -n ${MAXL:-80}
