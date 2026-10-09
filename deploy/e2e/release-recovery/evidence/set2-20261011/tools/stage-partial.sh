#!/bin/bash
# set2: stage a cell that was stopped before the driver wrote its result (no driver SHA256SUMS exists): every file is
# copied and compared by SHA-256 against its source; only then are THIS run's overlay disks of that lab removed.
# usage: stage-partial.sh JOB LAB NODE CELL RUN
set -euo pipefail
job=$1; lab=/var/tmp/cp-release-drill-$2; node=$3; cell=$4; run=$5
R=/var/tmp/cp-set2-run; L=$R/logs
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set2-20261011'
[ -e $L/$job.end ] || { echo "refusing: job $job has not ended"; exit 2; }
pgrep -f "cp-release-drill-$2/" > /dev/null && { echo "refusing: a process of lab $2 is still running"; exit 2; }
src=$(ls -d $lab/evidence/$node/upd1/${cell}-*/ | tail -1)
dst="$E/$cell/$run"
[ -e "$dst" ] && { echo "refusing: $dst exists"; exit 2; }
mkdir -p "$dst/host"
cp -r "$src". "$dst/"
( cd "$src" && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > /tmp/set2-partial-src.sha256
( cd "$dst" && sha256sum -c --quiet /tmp/set2-partial-src.sha256 ) && echo "every staged file equals its source ($(wc -l < /tmp/set2-partial-src.sha256) files; the driver wrote no SHA256SUMS: it was stopped)" || { echo "COPY CHECK FAILED"; exit 3; }
cp /tmp/set2-partial-src.sha256 "$dst/host/staged-files.sha256"
for f in $lab/evidence/$node/*.json $lab/evidence/$node/*.log $lab/evidence/$node/worker-origin-manifest; do [ -f "$f" ] && cp "$f" "$dst/host/" || true; done
cp $lab/cells/*/fixture-plan.json "$dst/host/" 2>/dev/null || true
for x in start end rc out err; do cp $L/$job.$x "$dst/host/wrapper.$x.txt"; done
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2
cp $J/job-${job}.sh "$dst/host/job.sh"
h=$(cat $J/job-${job}.harness); o=${h/harness-/overlay-}
echo "harness=$R/$h (git archive faa5ef085, overlay $o $(sha256sum $R/$o/harness.diff | cut -c1-16))" > "$dst/host/harness.txt"
{ echo "lab=$lab"; basename "$src"; echo "stopped by hand before the result was written (see README)"; } > "$dst/host/lab.txt"
for d in $lab/cells/*/*/overlay.qcow2; do
  [ -f "$d" ] || continue
  b=$(stat -c %s "$d"); rm -f -- "$d"
  echo "$(date -u +%FT%TZ) removed $d bytes=$b (evidence staged at set2-20261011/$cell/$run, every staged file compared with its source)" | tee -a "$E/host/removals.txt"
done
find "$dst" -type f | wc -l
