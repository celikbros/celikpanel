#!/bin/bash
# set5: the one queue worker (cells run one after the other). usage: queue.sh ITEM...   ITEM = JOB:LAB:NODE:CELL:RUN
# For each item in order: the disk gate (gate.sh: the newest Windows C: reading by PowerShell Get-PSDrive C, younger
# than 120 s, at least 40 GiB) before the lab is prepared; the cell job, whose wrapper asks the gate once more
# immediately before the guests are started; then the staging of its evidence and the removal of this run's
# overlay disks and base-image copies of that lab (stage.sh). A refused gate stops the queue: nothing is deleted
# to make room.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set5-run; L=$R/logs
log() { echo "$(date -u +%FT%TZ) $*" >> $L/queue.log; echo "$(date -u +%FT%TZ) $*" >> $R/progress.txt; }
for item in "$@"; do
  IFS=: read -r job lab node cell run <<< "$item"
  [ -e $R/queue.stop ] && { log "queue.stop present: $job and the rest of the list were NOT started"; exit 0; }
  [ -e $L/$job.start ] && { log "refusing $job: already started"; continue; }
  if ! bash $J/gate.sh "$cell $run (lab prepare)"; then
    log "NOT started $job: the disk gate refused ($(tail -n 1 $R/c-drive-cells.txt)); the queue stops here"
    exit 0
  fi
  [ -e $R/evidence-name.txt ] || echo "set5-$(date -u +%Y%m%d)" > $R/evidence-name.txt
  tr -d '\r' < $J/job-$job.sh > $R/job-$job.sh
  date -u +%FT%TZ > $L/$job.start
  log "start $job"
  ( cd $R; bash $R/job-$job.sh > $L/$job.out 2> $L/$job.err; echo $? > $L/$job.rc; date -u +%FT%TZ > $L/$job.end )
  log "end $job rc=$(cat $L/$job.rc)"
  bash $J/stage.sh $job $lab $node $cell $run > $L/stage-$job.txt 2>&1; src=$?
  log "staged $job rc=$src $(grep -m1 '^overall' $L/stage-$job.txt)"
  [ $src -eq 0 ] || { log "staging of $job did not complete: the queue stops here"; exit 0; }
done
log "queue done"
