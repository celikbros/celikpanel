#!/bin/bash
# usage: go-cell.sh SHORT CELL cur|a80 LAB PORT
set -eu
case $3 in cur) A=/var/tmp/cp-upd1-build/20261001t221215z/upd1-artifacts.json;; a80) A=/var/tmp/cp-upd1-build/20261001t221533z/upd1-artifacts.json;; *) echo bad; exit 2;; esac
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd11/mkcell-h21.sh $1 $2 $A $4 $5
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd11/startcell.sh $1
