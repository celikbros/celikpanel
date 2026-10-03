#!/bin/bash
# upd13: stage ONE finished cell run into the repository evidence folder (host only; read-only on the lab).
# usage: stagecell13.sh SHORT:LAB:NODE:CELLDIR:RUN
#   CELLDIR is the folder under the evidence root (part 2 cells live under part2-alpha80/<cell>).
# Refuses an existing run folder. Prints "STAGED-OK <dir>" only when the driver's own SHA256SUMS verifies.
set -u
R=/var/tmp/cp-upd13-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
IFS=: read -r short lab node celldir run <<< "$1"
L=/var/tmp/cp-release-drill-$lab
D="$S/$celldir/$run"
[ -e "$D" ] && { echo "refusing: $D exists"; exit 2; }
[ -e $R/logs/cell-$short.end ] || { echo "refusing: cell-$short has not ended"; exit 2; }
mkdir -p "$D/host" "$D/side"
ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
cp -a $ev/. "$D/"
ok=0
( cd "$D" && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 ) && ok=1
echo "$celldir/$run driver SHA256SUMS ok=$ok ($(wc -l < "$D/SHA256SUMS") entries)"
for x in start end rc out err; do cp -p $R/logs/cell-$short.$x "$D/host/wrapper.$x.txt" 2>/dev/null; done
cp -p $R/jobs/job-cell-$short.sh "$D/host/job.sh"
cp -p $R/jobs/job-cell-$short.harness "$D/host/harness.txt"
echo "lab=$L" > "$D/host/lab.txt"; basename $ev >> "$D/host/lab.txt"
for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp -p "$f" "$D/host/"; done
cp -p $L/lab.json $L/current-worker-baseline-$node-current-worker-baseline-result.json \
      $L/current-worker-baseline-$node-current-worker-baseline.log "$D/host/" 2>/dev/null
cp -p $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest "$D/host/" 2>/dev/null
cp -p $L/cells/*/fixture-plan.json "$D/host/" 2>/dev/null
PYTHONDONTWRITEBYTECODE=1 python3 $P/ext13.py $lab > "$D/side/extract.txt" 2>&1
PYTHONDONTWRITEBYTECODE=1 python3 $P/cpaths.py $lab > "$D/side/changed-paths.txt" 2>&1
PYTHONDONTWRITEBYTECODE=1 python3 $P/dml13.py "$D" > "$D/side/deferred-mail-timeline.txt" 2>&1
[ $ok -eq 1 ] && echo "STAGED-OK $D" || echo "STAGED-WITH-SUMS-FAILURE $D"
