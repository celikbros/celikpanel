#!/bin/bash
# set6: stage one cell's evidence into the repository's evidence folder; verify the driver's SHA256SUMS on the staged
# copy; pass the staged copy through the digest rules once more, plain and inside base64 text, with the values of the
# lab's own raw records known (the host-side records are not written through the driver's redactor); and only then
# remove THIS run's overlay disks and base-image links of that lab (each listed in host/removals.txt).
# usage: stage.sh JOB LAB NODE CELL RUN
set -euo pipefail
job=$1; labname=$2; node=$3; cell=$4; run=$5
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set6-run; L=$R/logs
lab=/var/tmp/cp-release-drill-$labname
CACHE=/var/tmp/cp-v3n28/images
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
case $labname in s6-*) ;; *) echo "refusing: $labname is not a lab of this run"; exit 2;; esac
[ -e $L/$job.start ] || { echo "refusing: no job $job of this run"; exit 2; }
[ -e $L/$job.end ] || { echo "refusing: job $job has not ended"; exit 2; }
pgrep -f "cp-release-drill-$labname/" > /dev/null && { echo "refusing: a process of lab $labname is still running"; pgrep -af "cp-release-drill-$labname/" | cut -c1-120; exit 2; }
h=$(grep -o 'harness-[a-z0-9]*' $R/job-$job.sh | head -n 1); o=${h/harness-/overlay-}
REDACT=$R/$h/deploy/e2e/release-recovery/set6_redact.py
case $cell in set6-*) group=fresh-install;; *) group=update-alpha81;; esac
dst="$E/$group/$cell/$run"
[ -e "$dst" ] && { echo "refusing: $dst exists"; exit 2; }
mkdir -p "$dst/host" "$E/host"
src=$(ls -d $lab/evidence/$node/upd1/${cell}-*/ 2>/dev/null | tail -1 || true)
if [ -n "$src" ]; then
  cp -r "$src". "$dst/"
  if [ -f "$dst/SHA256SUMS" ]; then
    ( cd "$dst" && sha256sum -c --quiet SHA256SUMS ) && echo "driver SHA256SUMS verified on the staged copy ($(wc -l < "$dst/SHA256SUMS") files)" || { echo "SHA256SUMS FAILED"; exit 3; }
  else
    # a run that was stopped before its result: every staged file is compared with its source one by one
    ( cd "$src" && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > $L/staged-files-$job.sha256
    ( cd "$dst" && sha256sum -c --quiet $L/staged-files-$job.sha256 ) && echo "no driver SHA256SUMS (run without a result); $(wc -l < $L/staged-files-$job.sha256) staged files equal their source" || { echo "STAGED COPY DIFFERS"; exit 3; }
  fi
  { echo "lab=$lab"; basename "$src"; } > "$dst/host/lab.txt"
else
  echo "no driver evidence directory in $lab (the cell stopped before the driver wrote anything)"
  echo "lab=$lab (no driver run directory)" > "$dst/host/lab.txt"
fi
for f in $lab/evidence/$node/*.json $lab/evidence/$node/*.log $lab/evidence/$node/worker-origin-manifest; do [ -f "$f" ] && cp "$f" "$dst/host/" || true; done
cp $lab/cells/*/fixture-plan.json "$dst/host/" 2>/dev/null || true
for x in start end rc out err; do cp $L/$job.$x "$dst/host/wrapper.$x.txt"; done
cp $R/job-$job.sh "$dst/host/job.sh"
echo "harness=$R/$h (git archive 72b879eea without earlier evidence, overlay $o, harness.diff sha256 $(sha256sum $R/$o/harness.diff | cut -c1-64))" > "$dst/host/harness.txt"
grep " $cell $run " $R/c-drive-cells.txt > "$dst/host/c-drive-gate.txt" || true
# digests of tokens: the values are learned from the lab's raw records (which stay on the WSL host), plain and inside
# base64 text, and removed from every staged file; the report holds counts only. A driver file that changes here gets
# its SHA256SUMS line rewritten.
if [ -d "$lab/evidence" ]; then
  python3 -I $REDACT sweep --learn-from "$lab/evidence" "$dst" > $L/stage-sweep-$job.json
else
  python3 -I $REDACT sweep "$dst" > $L/stage-sweep-$job.json
fi
python3 -I $J/stagesums.py "$dst" $L/stage-sweep-$job.json
if [ -d "$lab/evidence" ]; then
  python3 -I $REDACT count --learn-from "$lab/evidence" "$dst" > $L/stage-count-$job.json || { echo "A DIGEST OF A TOKEN IS STILL PRESENT after the sweep; overlays are kept"; exit 4; }
fi
echo "count after the sweep: $(python3 -I $J/stagecount.py $L/stage-count-$job.json 2>/dev/null || echo 'not counted')"
[ ! -f "$dst/SHA256SUMS" ] || ( cd "$dst" && sha256sum -c --quiet SHA256SUMS && echo "driver SHA256SUMS verified again after the sweep" )
for d in $lab/cells/*/*/overlay.qcow2; do
  [ -f "$d" ] || continue
  b=$(stat -c %s "$d"); a=$(du -B1 "$d" | cut -f1)
  rm -f -- "${d:?}"
  echo "$(date -u +%FT%TZ) removed $d bytes=$b allocated=$a (overlay disk of this run's lab, on the WSL disk; evidence staged at $NAME/$group/$cell/$run, checksums verified)" | tee -a "$E/host/removals.txt"
done
for d in $lab/images/*; do
  [ -f "$d" ] || continue
  c="$CACHE/$(basename "$d")"
  if [ -f "$c" ] && [ "$d" -ef "$c" ]; then
    rm -f -- "${d:?}"
    echo "$(date -u +%FT%TZ) removed $d (this lab's hard link to the cached base image $c; the cached file stays, link count now $(stat -c %h "$c"))" | tee -a "$E/host/removals.txt"
  elif [ -f "$c" ] && cmp -s "$d" "$c"; then
    b=$(stat -c %s "$d"); rm -f -- "${d:?}"
    echo "$(date -u +%FT%TZ) removed $d bytes=$b (this lab's own copy of the base image, byte-equal to the cache file $c, which stays)" | tee -a "$E/host/removals.txt"
  else
    echo "$(date -u +%FT%TZ) KEPT $d (neither a link to nor byte-equal to a cache file)" | tee -a "$E/host/removals.txt"
  fi
done
find "$dst" -type f | wc -l
python3 -I $J/stageprint.py "$dst/result.json" || true
