#!/bin/bash
# set4: stage one cell's evidence into the repository's evidence folder, verify the driver's SHA256SUMS on the staged
# copy, and only then remove THIS run's overlay disks of that lab (each listed in host/removals.txt).
# usage: stage.sh JOB LAB NODE CELL RUN [PREFIX]     (PREFIX: a sub-folder such as update-alpha81)
set -euo pipefail
job=$1; lab=/var/tmp/cp-release-drill-$2; node=$3; cell=$4; run=$5; prefix=${6:-}
R=/var/tmp/cp-set4-run; L=$R/logs
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set4-20261009'
case $2 in s4-*) ;; *) echo "refusing: $2 is not a lab of this run"; exit 2;; esac
[ -e $L/$job.start ] || { echo "refusing: no job $job of this run"; exit 2; }
[ -e $L/$job.end ] || { echo "refusing: job $job has not ended"; exit 2; }
pgrep -f "cp-release-drill-$2/" > /dev/null && { echo "refusing: a process of lab $2 is still running"; pgrep -af "cp-release-drill-$2/" | cut -c1-120; exit 2; }
src=$(ls -d $lab/evidence/$node/upd1/${cell}-*/ | tail -1)
dst="$E/${prefix:+$prefix/}$cell/$run"
[ -e "$dst" ] && { echo "refusing: $dst exists"; exit 2; }
mkdir -p "$dst/host"
cp -r "$src". "$dst/"
for f in $lab/evidence/$node/*.json $lab/evidence/$node/*.log $lab/evidence/$node/worker-origin-manifest; do [ -f "$f" ] && cp "$f" "$dst/host/" || true; done
cp $lab/cells/*/fixture-plan.json "$dst/host/" 2>/dev/null || true
for x in start end rc out err; do cp $L/$job.$x "$dst/host/wrapper.$x.txt"; done
cp $R/job-$job.sh "$dst/host/job.sh"
h=$(grep -o 'harness-[a-z0-9]*' $R/job-$job.sh | head -n 1); o=${h/harness-/overlay-}
echo "harness=$R/$h (git archive 557b554eb, overlay $o $(sha256sum $R/$o/harness.diff | cut -c1-16))" > "$dst/host/harness.txt"
{ echo "lab=$lab"; basename "$src"; } > "$dst/host/lab.txt"
if [ -f "$dst/SHA256SUMS" ]; then
  ( cd "$dst" && sha256sum -c --quiet SHA256SUMS ) && echo "driver SHA256SUMS verified on the staged copy ($(wc -l < "$dst/SHA256SUMS") files)" || { echo "SHA256SUMS FAILED"; exit 3; }
else
  # a run that was stopped before its result: every staged file is compared with its source one by one
  ( cd "$src" && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > "$dst/host/staged-files.sha256"
  ( cd "$dst" && sha256sum -c --quiet host/staged-files.sha256 ) && echo "no driver SHA256SUMS (run without a result); $(wc -l < "$dst/host/staged-files.sha256") staged files equal their source" || { echo "STAGED COPY DIFFERS"; exit 3; }
fi
mkdir -p "$E/host"
for d in $lab/cells/*/*/overlay.qcow2; do
  [ -f "$d" ] || continue
  b=$(stat -c %s "$d"); rm -f -- "$d"
  echo "$(date -u +%FT%TZ) removed $d bytes=$b (evidence staged at set4-20261009/${prefix:+$prefix/}$cell/$run, checksums verified)" | tee -a "$E/host/removals.txt"
done
find "$dst" -type f | wc -l
python3 "$(dirname "$0")/stageprint.py" "$dst/result.json"
