#!/bin/bash
# usage: go-cell.sh SHORT CELL cur LAB PORT
set -eu
case $3 in cur) A=/var/tmp/cp-upd1-build/20261002t140055z/upd1-artifacts.json;; *) echo bad; exit 2;; esac
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd12/mkcell.sh $1 $2 $A $4 $5
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd12/startcell.sh $1
