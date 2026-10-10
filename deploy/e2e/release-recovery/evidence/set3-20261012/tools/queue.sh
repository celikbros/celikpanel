#!/bin/bash
# set3: one queue worker. usage: queue.sh WORKER GATE_JOB|- ITEM...   ITEM = JOB:LAB:NODE:CELL:RUN[:PREFIX]
# For each item in order: wait for the gate (first item only), take the newest Windows C: reading written by
# cwatch.ps1 (PowerShell Get-PSDrive C; must be younger than 3 minutes), start the cell only with >= 40 GiB free and
# no below-30 flag, run the cell job, then stage its evidence and remove the lab's overlay disks (stage.sh).
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
R=/var/tmp/cp-set3-run; L=$R/logs
w=$1; gate=$2; shift 2
log() { echo "$(date -u +%FT%TZ) $w $*" | tee -a $L/queue.log; }
if [ "$gate" != "-" ]; then
  log "waiting for $gate"
  while [ ! -e $L/$gate.end ]; do sleep 10; [ -e $R/queue.stop ] && { log "stopped while waiting"; exit 0; }; done
fi
for item in "$@"; do
  IFS=: read -r job lab node cell run prefix <<< "$item"
  [ -e $R/queue.stop ] && { log "queue.stop present: $job and the rest of this worker's list were NOT started"; exit 0; }
  [ -e $J/c-drive.below30 ] && { log "C: went under 30 GiB: $job and the rest were NOT started"; exit 0; }
  reading=$(tail -n 1 $J/c-drive-watch.txt | tr -d '\r')
  stamp=$(echo "$reading" | cut -d' ' -f1); gib=$(echo "$reading" | sed -n 's/.*free_GiB=\([0-9.]*\).*/\1/p')
  age=$(( $(date -u +%s) - $(date -u -d "$stamp" +%s) ))
  echo "$(date -u +%FT%TZ) before $cell${prefix:+ ($prefix)} $run free_GiB=$gib (Get-PSDrive C reading of $stamp, ${age}s old)" >> $R/c-drive-cells.txt
  cp $R/c-drive-cells.txt $J/c-drive-cells.txt 2>/dev/null
  if [ "$age" -gt 180 ] || [ -z "$gib" ] || [ "${gib%.*}" -lt 40 ]; then log "NOT started $job: C: reading '$reading' (age ${age}s) does not allow a cell"; exit 0; fi
  [ -e $L/$job.start ] && { log "refusing $job: already started"; continue; }
  tr -d '\r' < $J/job-$job.sh > $R/job-$job.sh
  date -u +%FT%TZ > $L/$job.start
  log "start $job (C: $gib GiB)"
  ( cd $R; bash $R/job-$job.sh > $L/$job.out 2> $L/$job.err; echo $? > $L/$job.rc; date -u +%FT%TZ > $L/$job.end )
  log "end $job rc=$(cat $L/$job.rc)"
  bash $J/stage.sh $job $lab $node $cell $run $prefix > $L/stage-$job.txt 2>&1; log "staged $job rc=$? $(grep -m1 '^overall' $L/stage-$job.txt)"
done
log "worker done"
