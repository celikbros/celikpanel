#!/bin/bash
# usage: waitcell.sh SHORT LAB MAXSECONDS [STOPSTEP] -> returns at job end, a failed/inconclusive step, STOPSTEP seen, or the limit
short=$1 lab=$2 max=$3 stop=${4:-}
L=/var/tmp/cp-upd9-run/logs
start=$(date +%s)
while true; do
  [ -e $L/cell-$short.end ] && break
  ev=$(ls -d /var/tmp/cp-release-drill-$lab/evidence/*/upd1/*/ 2>/dev/null | tail -1)
  if [ -n "$ev" ]; then
    bad=$(grep -l -E '"verdict": "(failed|inconclusive)"' $ev/steps/*/step.json 2>/dev/null | head -1)
    [ -n "$bad" ] && break
    [ -n "$stop" ] && ls $ev/steps 2>/dev/null | grep -q -- "$stop" && break
  fi
  [ $(( $(date +%s) - start )) -ge $max ] && break
  sleep 20
done
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd9/peek.sh $short $lab
