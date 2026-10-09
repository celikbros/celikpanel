#!/bin/bash
# set5: stage one cell's evidence into the repository's evidence folder; verify the driver's SHA256SUMS on the staged
# copy; pass the staged copy through the token-digest rules once more with the values of the lab's own raw records
# known (the host-side records are not written through the driver's redactor); and only then remove THIS run's
# overlay disks, tmpfs mounts and base-image links of that lab (each listed in host/removals.txt).
# usage: stage.sh JOB LAB NODE CELL RUN
set -euo pipefail
job=$1; labname=$2; node=$3; cell=$4; run=$5
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set5-run; L=$R/logs
lab=/var/tmp/cp-release-drill-$labname
CACHE=/var/tmp/cp-v3n28/images
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
case $labname in s5-*) ;; *) echo "refusing: $labname is not a lab of this run"; exit 2;; esac
[ -e $L/$job.start ] || { echo "refusing: no job $job of this run"; exit 2; }
[ -e $L/$job.end ] || { echo "refusing: job $job has not ended"; exit 2; }
pgrep -f "cp-release-drill-$labname/" > /dev/null && { echo "refusing: a process of lab $labname is still running"; pgrep -af "cp-release-drill-$labname/" | cut -c1-120; exit 2; }
h=$(grep -o 'harness-[a-z0-9]*' $R/job-$job.sh | head -n 1); o=${h/harness-/overlay-}
REDACT=$R/$h/deploy/e2e/release-recovery/set5_redact.py
dst="$E/update-alpha81/$cell/$run"
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
echo "harness=$R/$h (git archive 67b62cc0f without earlier evidence, overlay $o, harness.diff sha256 $(sha256sum $R/$o/harness.diff | cut -c1-64))" > "$dst/host/harness.txt"
grep " $cell $run " $R/c-drive-cells.txt > "$dst/host/c-drive-gate.txt" || true
# token digests: the values are learned from the lab's raw records (which stay on the WSL host) and removed from
# every staged file; the report holds counts only. A driver file that changes here gets its SHA256SUMS line rewritten.
if [ -d "$lab/evidence" ]; then
  python3 -I $REDACT sweep --learn-from "$lab/evidence" "$dst" > $L/stage-sweep-$job.json
else
  python3 -I $REDACT sweep "$dst" > $L/stage-sweep-$job.json
fi
python3 -I - "$dst" $L/stage-sweep-$job.json <<'PY'
import hashlib, json, os, sys
dst, report = sys.argv[1], json.load(open(sys.argv[2]))
changed = report["files_changed"]
driver = sorted(name for name in changed if not name.startswith("host/"))
sums = os.path.join(dst, "SHA256SUMS")
rewritten = []
if driver and os.path.isfile(sums):
    lines = open(sums, encoding="ascii").read().splitlines()
    for index, line in enumerate(lines):
        digest, name = line.split("  ", 1)
        if name in driver:
            lines[index] = hashlib.sha256(open(os.path.join(dst, name), "rb").read()).hexdigest() + "  " + name
            rewritten.append(name)
    open(sums, "w", encoding="ascii", newline="\n").write("\n".join(lines) + "\n")
report["at_staging"] = {"host_side_files_changed": sorted(name for name in changed if name.startswith("host/")),
                        "driver_files_changed": driver, "driver_sha256sums_lines_rewritten": rewritten,
                        "values_learned_from": "the lab's own raw records on the WSL host (not retained here)"}
json.dump(report, open(os.path.join(dst, "host", "token-digest-sweep-at-staging.json"), "w", newline="\n"), indent=2, sort_keys=True)
print("token-digest sweep at staging: host-side files changed %d, driver files changed %d, places %s" % (
    len(report["at_staging"]["host_side_files_changed"]), len(driver), json.dumps(report["places_by_class"], sort_keys=True)))
PY
if [ -d "$lab/evidence" ]; then
  python3 -I $REDACT count --learn-from "$lab/evidence" "$dst" > $L/stage-count-$job.json || { echo "TOKEN DIGEST STILL PRESENT after the sweep; overlays are kept"; exit 4; }
fi
echo "token-digest count after the sweep: $(python3 -I -c "import json,sys;d=json.load(open(sys.argv[1]));print(len(d['files_that_would_change']), 'file(s) would change, values known:', d['distinct_values_known'])" $L/stage-count-$job.json 2>/dev/null || echo 'not counted')"
[ ! -f "$dst/SHA256SUMS" ] || ( cd "$dst" && sha256sum -c --quiet SHA256SUMS && echo "driver SHA256SUMS verified again after the sweep" )
cp $lab/ram-node-directories.txt "$dst/host/" 2>/dev/null || true
for d in $lab/cells/*/*/overlay.qcow2; do
  [ -f "$d" ] || continue
  b=$(stat -c %s "$d"); where="on the WSL disk"; mountpoint -q "$(dirname "$d")" && where="on this lab's tmpfs mount (RAM)"
  rm -f -- "${d:?}"
  echo "$(date -u +%FT%TZ) removed $d bytes=$b (overlay disk of this run's lab, $where; evidence staged at $NAME/update-alpha81/$cell/$run, checksums verified)" | tee -a "$E/host/removals.txt"
done
# the node directories were tmpfs mounts (ramnodes.sh): what is left in them (serial log, seed, QEMU's files) goes back
# to the disk under the lab, then the mounts of this lab are released
for nd in $lab/cells/*/*/; do
  nd=${nd%/}
  mountpoint -q "$nd" || continue
  keep=$R/ramstage/$labname-$(basename "$nd")-after
  mkdir -m 700 "$keep" && cp -a "$nd/." "$keep/"
  used=$(findmnt -rn -o USED "$nd")
  if umount "$nd"; then
    cp -a "$keep/." "$nd/"
    echo "$(date -u +%FT%TZ) released the tmpfs mount $nd (it held $used after the overlay was removed; its remaining files were put back on the disk)" | tee -a "$E/host/removals.txt"
  else
    echo "$(date -u +%FT%TZ) COULD NOT release the tmpfs mount $nd" | tee -a "$E/host/removals.txt"; exit 5
  fi
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
