#!/bin/bash
# upd13: remove ONLY the overlay disks of one lab that this run created, after its evidence was staged and the
# staged copy's driver SHA256SUMS verified. usage: rmoverlay13.sh LAB CELLDIR RUN
# Refuses: a lab not named upd13-*, a lab whose wrapper has not ended, a running QEMU, a staged copy that does
# not verify. Appends every removal (path, bytes, SHA-256 not computed) to the scratch removals log.
set -u
lab=$1 celldir=$2 run=$3
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
L=/var/tmp/cp-release-drill-$lab
case $lab in upd13-*) ;; *) echo "refusing: $lab is not a lab of this run"; exit 2;; esac
[ -d "$L" ] || { echo "refusing: no $L"; exit 2; }
grep -q " $lab [0-9]" /var/tmp/cp-upd13-run/jobs/*.sh 2>/dev/null || { echo "refusing: no upd13 job created $lab"; exit 2; }
if pgrep -f "qemu-system.*$L" > /dev/null; then echo "refusing: QEMU of $L running"; exit 2; fi
D="$S/$celldir/$run"
( cd "$D" && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 ) || { echo "refusing: staged $D does not verify"; exit 2; }
for f in $L/cells/*/*/overlay.qcow2; do
  [ -f "$f" ] || continue
  sz=$(stat -c %s "$f")
  rm -f -- "$f" && echo "$(date -u +%FT%TZ) removed $f bytes=$sz (evidence staged at upd13-20261002/$celldir/$run, driver SHA256SUMS verified)" | tee -a $P/removals.txt
done
du -sh $L
