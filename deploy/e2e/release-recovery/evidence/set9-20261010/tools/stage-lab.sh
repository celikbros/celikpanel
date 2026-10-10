#!/bin/bash
# set9: stage one lab's evidence into the repository's evidence folder: the driver's run directory (its SHA256SUMS
# verified on the staged copy), the lab's host-side records, and the browser's records of that lab (screenshots,
# samples, network, events; the Chrome profile directory is NOT staged: it holds the session). The staged copy is
# passed through set6's digest rules once more (plain and inside base64 text) with the values of the lab's own raw
# records known; only then are THIS run's overlay disks and base-image links of that lab removed (host/removals.txt).
# usage: stage-lab.sh JOB LAB GROUP BROWSER_DIR
set -euo pipefail
job=$1; labname=$2; group=$3; browser=$4
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set9-run; L=$R/logs
lab=/var/tmp/cp-release-drill-$labname
CACHE=/var/tmp/cp-v3n28/images
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
case $labname in s9-*) ;; *) echo "refusing: $labname is not a lab of this run"; exit 2;; esac
[ -e $L/$job.end ] || { echo "refusing: job $job has not ended"; exit 2; }
pgrep -f "cp-release-drill-$labname/" > /dev/null && { echo "refusing: a process of lab $labname is still running"; exit 2; }
REDACT=$R/harness-b/deploy/e2e/release-recovery/set6_redact.py
dst="$E/$group"
[ -e "$dst/driver" ] && { echo "refusing: $dst/driver exists"; exit 2; }
mkdir -p "$dst/driver" "$dst/host" "$dst/browser" "$E/host"
src=$(ls -d $lab/evidence/debian13/upd1/upd1-debian13-good-*/ | tail -1)
cp -r "$src". "$dst/driver/"
( cd "$dst/driver" && sha256sum -c --quiet SHA256SUMS ) && echo "driver SHA256SUMS verified on the staged copy ($(wc -l < "$dst/driver/SHA256SUMS") files)"
{ echo "lab=$lab"; basename "$src"; } > "$dst/host/lab.txt"
for f in $lab/evidence/debian13/*.json $lab/evidence/debian13/*.log $lab/evidence/debian13/worker-origin-manifest; do [ -f "$f" ] && cp "$f" "$dst/host/" || true; done
cp $lab/cells/*/fixture-plan.json "$dst/host/" 2>/dev/null || true
for x in start end rc out err; do cp $L/$job.$x "$dst/host/wrapper.$x.txt"; done
cp $R/job-$job.sh "$dst/host/job.sh"
grep "$labname" $R/c-drive-cells.txt > "$dst/host/c-drive-gate.txt" || true
# the browser's records: everything but the Chrome profile
( cd "$browser" && find . -path './.chrome-profile-*' -prune -o -type f -print ) | sort > $L/browser-files-$job.txt
( cd "$browser" && tar -cf - --exclude='./.chrome-profile-*' . ) | ( cd "$dst/browser" && tar -xf - )
echo "browser files staged: $(wc -l < $L/browser-files-$job.txt) (Chrome profile directories not staged: $(ls -d "$browser"/.chrome-profile-* 2>/dev/null | wc -l))"
python3 -I $REDACT sweep --learn-from "$lab/evidence" "$dst" > $L/stage-sweep-$job.json
python3 -I - "$L/stage-sweep-$job.json" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
print("sweep: files changed", len(r["files_changed"]), "inside base64", len(r["files_changed_inside_base64_text"]), "places", json.dumps(r.get("places_by_class"), sort_keys=True))
PY
cp $L/stage-sweep-$job.json "$dst/host/digest-sweep-at-staging.json"
python3 -I $REDACT count --learn-from "$lab/evidence" "$dst" > $L/stage-count-$job.json || { echo "A DIGEST OF A TOKEN IS STILL PRESENT after the sweep; overlays are kept"; exit 4; }
( cd "$dst/driver" && sha256sum -c --quiet SHA256SUMS ) && echo "driver SHA256SUMS verified again after the sweep" || echo "driver SHA256SUMS: a file changed in the sweep (see host/digest-sweep-at-staging.json)"
for d in $lab/cells/*/*/overlay.qcow2; do
  [ -f "$d" ] || continue
  b=$(stat -c %s "$d"); a=$(du -B1 "$d" | cut -f1)
  rm -f -- "${d:?}"
  echo "$(date -u +%FT%TZ) removed $d bytes=$b allocated=$a (overlay disk of this run's lab; evidence staged at $NAME/$group)" | tee -a "$E/host/removals.txt"
done
for d in $lab/images/*; do
  [ -f "$d" ] || continue
  c="$CACHE/$(basename "$d")"
  if [ -f "$c" ] && [ "$d" -ef "$c" ]; then
    rm -f -- "${d:?}"
    echo "$(date -u +%FT%TZ) removed $d (this lab's hard link to the cached base image $c; the cached file stays, link count now $(stat -c %h "$c"))" | tee -a "$E/host/removals.txt"
  else
    echo "$(date -u +%FT%TZ) KEPT $d (not a link to a cache file)" | tee -a "$E/host/removals.txt"
  fi
done
find "$dst" -type f | wc -l
