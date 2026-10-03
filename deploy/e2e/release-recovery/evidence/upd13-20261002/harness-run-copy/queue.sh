#!/bin/bash
# upd13: run cells one after another (detached via bg.sh). usage: queue.sh LISTFILE
# Each line: SHORT CELL BUILD(cur|a80) LAB PORT CELLDIR NODE [RUN, default run-a]. Before each cell: C: reading through PowerShell
# Get-PSDrive (cdrive.ps1); a cell starts only with >= 40 GiB free and no QEMU running; during a cell C: is read every
# 10 min and a reading under 30 GiB writes the stop file (no further cell starts). After each cell: stage-batch
# (stage, verify the driver's SHA256SUMS, then remove only that lab's overlay disks). The stop file stops the queue
# before the next cell.
set -u
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
R=/var/tmp/cp-upd13-run
L=$R/logs
STOP=$P/queue.stop
PS=/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe
PSF='C:\Users\alice\AppData\Local\Temp\claude\c--CELIKBROS-PROJECTS-celikpanel\ab34f56e-94b5-4834-a9ea-4e8dbe057e7f\scratchpad\upd13\cdrive.ps1'
cdrive() { timeout 60 $PS -NoProfile -ExecutionPolicy Bypass -File "$PSF" -Label "$1" | tr -d '\r'; }
while read -r -u 3 short cell build lab port celldir node run; do
  run=${run:-run-a}
  [ -n "$short" ] || continue
  case $short in \#*) continue;; esac
  if [ -e $STOP ]; then echo "$(date -u +%FT%TZ) queue stopped by the stop file before $short ($(cat $STOP))"; break; fi
  line=$(cdrive before-cell-$short); echo "$line"
  free=$(echo "$line" | sed -n 's/.*free_bytes=\([0-9]*\).*/\1/p')
  if [ -z "$free" ] || [ "$free" -lt 42949672960 ]; then echo "$(date -u +%FT%TZ) NOT STARTED $short: C: reading '$line' (< 40 GiB or unreadable)"; echo "low disk before $short" > $STOP; break; fi
  while pgrep -f qemu-system > /dev/null; do echo "waiting: qemu running"; sleep 20; done
  [ -e $L/cell-$short.start ] && { echo "refusing: cell-$short already started"; continue; }
  art=$(cat $R/art-$build.txt)
  bash $P/mkcell.sh $short $cell $art $lab $port > /dev/null
  echo "$(date -u +%FT%TZ) start cell-$short $cell $build $lab $port"
  date -u +%FT%TZ > $L/cell-$short.start
  ( while sleep 600; do l=$(cdrive during-cell-$short); f=$(echo "$l" | sed -n 's/.*free_bytes=\([0-9]*\).*/\1/p'); [ -n "$f" ] && [ "$f" -lt 32212254720 ] && echo "C: under 30 GiB during $short: $l" > $STOP; done ) &
  mon=$!
  bash $R/jobs/job-cell-$short.sh < /dev/null > $L/cell-$short.out 2> $L/cell-$short.err; echo $? > $L/cell-$short.rc
  date -u +%FT%TZ > $L/cell-$short.end
  kill $mon 2>/dev/null; wait $mon 2>/dev/null
  echo "$(date -u +%FT%TZ) end cell-$short rc=$(cat $L/cell-$short.rc): $(tail -n 1 $L/cell-$short.out | cut -c1-300)"
  bash $P/stage-batch.sh $short:$lab:$node:$celldir:$run 2>&1
done 3< "$1"
echo "$(date -u +%FT%TZ) queue done"
