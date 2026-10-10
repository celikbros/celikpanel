#!/bin/bash
# set1: stage ONE finished cell run into the repository evidence folder (host only; read-only on the lab).
# usage: stagecell.sh SHORT:LAB:NODE:CELLDIR:RUN
# Refuses an existing run folder. Prints "STAGED-OK <dir>" only when the driver's own SHA256SUMS verifies.
set -u
R=/var/tmp/cp-set1-run
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set1-20261010"
IFS=: read -r short lab node celldir run <<< "$1"
L=/var/tmp/cp-release-drill-$lab
D="$S/$celldir/$run"
[ -e "$D" ] && { echo "refusing: $D exists"; exit 2; }
[ -e $R/logs/cell-$short.end ] || { echo "refusing: cell-$short has not ended"; exit 2; }
mkdir -p "$D/host"
ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
cp -a $ev/. "$D/"
ok=0
if [ -f "$D/SHA256SUMS" ]; then ( cd "$D" && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 ) && ok=1; fi
echo "$celldir/$run driver SHA256SUMS ok=$ok ($(wc -l < "$D/SHA256SUMS" 2>/dev/null) entries)"
for x in start end rc out err; do cp -p $R/logs/cell-$short.$x "$D/host/wrapper.$x.txt" 2>/dev/null; done
cp -p $R/jobs/job-cell-$short.sh "$D/host/job.sh"
cp -p $R/jobs/job-cell-$short.harness "$D/host/harness.txt"
echo "lab=$L" > "$D/host/lab.txt"; basename $ev >> "$D/host/lab.txt"
for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp -p "$f" "$D/host/"; done
cp -p $L/current-worker-baseline-$node-current-worker-baseline-result.json \
      $L/current-worker-baseline-$node-current-worker-baseline.log "$D/host/" 2>/dev/null
cp -p $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest "$D/host/" 2>/dev/null
cp -p $L/cells/*/fixture-plan.json "$D/host/" 2>/dev/null
find "$D" -type f | wc -l; du -sh "$D" | cut -f1
[ $ok -eq 1 ] && echo "STAGED-OK $D" || echo "STAGED-WITH-SUMS-FAILURE $D"
