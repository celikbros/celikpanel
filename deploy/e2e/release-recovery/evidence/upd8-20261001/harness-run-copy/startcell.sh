#!/bin/bash
# usage: startcell.sh SHORT   -> refuses when any QEMU is running or another cell job is still running
set -u
short=$1
L=/var/tmp/cp-upd8-run/logs
if pgrep -f qemu-system > /dev/null; then echo "refusing: qemu running"; pgrep -a qemu | cut -c1-120; exit 2; fi
for s in $L/cell-*.start; do
  [ -e "$s" ] || continue
  n=$(basename $s .start)
  [ -e $L/$n.end ] || { echo "refusing: $n still running"; exit 2; }
done
free -m | sed -n 2p
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd8/bg.sh cell-$short /var/tmp/cp-upd8-run/jobs/job-cell-$short.sh
