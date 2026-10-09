#!/bin/bash
# set4: the one queue worker (cells run one after the other). usage: queue.sh ITEM...   ITEM = JOB:LAB:NODE:CELL:RUN[:PREFIX]
# For each item in order: take the newest Windows C: reading written by cwatch.ps1 (PowerShell Get-PSDrive C; must be
# younger than 3 minutes), start the cell only with >= 40 GiB free and no below-30 flag, run the cell job, then stage
# its evidence and remove the lab's overlay disks (stage.sh).
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set4-run; L=$R/logs
log() { echo "$(date -u +%FT%TZ) $*" >> $L/queue.log; echo "$(date -u +%FT%TZ) $*" >> $R/progress.txt; }
for item in "$@"; do
  IFS=: read -r job lab node cell run prefix <<< "$item"
  [ -e $R/queue.stop ] && { log "queue.stop present: $job and the rest of the list were NOT started"; exit 0; }
  [ -e $J/c-drive.below30 ] && { log "C: went under 30 GiB: $job and the rest were NOT started"; exit 0; }
  reading=$(tail -n 1 $J/c-drive-watch.txt | tr -d '\r')
  stamp=$(echo "$reading" | cut -d' ' -f1); gib=$(echo "$reading" | sed -n 's/.*free_GiB=\([0-9.]*\).*/\1/p')
  age=$(( $(date -u +%s) - $(date -u -d "$stamp" +%s) ))
  echo "$(date -u +%FT%TZ) before $cell${prefix:+ ($prefix)} $run free_GiB=$gib (Get-PSDrive C reading of $stamp, ${age}s old)" >> $R/c-drive-cells.txt
  if [ "$age" -gt 180 ] || [ -z "$gib" ] || [ "${gib%.*}" -lt 40 ]; then log "NOT started $job: C: reading '$reading' (age ${age}s) does not allow a cell"; exit 0; fi
  [ -e $L/$job.start ] && { log "refusing $job: already started"; continue; }
  tr -d '\r' < $J/job-$job.sh > $R/job-$job.sh
  date -u +%FT%TZ > $L/$job.start
  log "start $job (C: $gib GiB)"
  ( cd $R; bash $R/job-$job.sh > $L/$job.out 2> $L/$job.err; echo $? > $L/$job.rc; date -u +%FT%TZ > $L/$job.end )
  log "end $job rc=$(cat $L/$job.rc)"
  bash $J/stage.sh $job $lab $node $cell $run $prefix > $L/stage-$job.txt 2>&1; log "staged $job rc=$? $(grep -m1 '^overall' $L/stage-$job.txt)"
done
log "queue done"
