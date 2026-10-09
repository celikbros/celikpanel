#!/bin/bash
# set5: the prepared, never-used overlay files. ramnodes.sh mounted a tmpfs over each node directory; the overlay
# file that `lab.py prepare` had made there (196992 bytes, an empty qcow2 that no guest ever opened) stayed hidden
# under the mount and came back when the mount was released. The used overlays (on the tmpfs) were removed by
# stage.sh. This removes the never-used ones too, in the labs and in the run's own staging copies, after checking
# each against its size and against the copy taken before the mount; every removal is listed.
set -uo pipefail
R=/var/tmp/cp-set5-run
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$(cat $R/evidence-name.txt)"
for f in /var/tmp/cp-release-drill-s5-*/cells/*/*/overlay.qcow2; do
  [ -f "$f" ] || continue
  lab=$(echo "$f" | cut -d/ -f4); node=$(basename "$(dirname "$f")")
  keep=$R/ramstage/$lab-$node-before/overlay.qcow2
  size=$(stat -c %s "$f")
  if [ "$size" -eq 196992 ] && [ -f "$keep" ] && cmp -s "$f" "$keep" && ! mountpoint -q "$(dirname "$f")"; then
    rm -f -- "${f:?}"
    echo "$(date -u +%FT%TZ) removed $f bytes=$size (the prepared overlay file no guest ever opened: it lay under this lab's tmpfs mount during the cell; byte-equal to the copy taken before the mount)" | tee -a "$E/host/removals.txt"
  else
    echo "$(date -u +%FT%TZ) KEPT $f bytes=$size (not the prepared, never-used file)" | tee -a "$E/host/removals.txt"
  fi
done
for f in $R/ramstage/cp-release-drill-s5-*-before/overlay.qcow2; do
  [ -f "$f" ] || continue
  size=$(stat -c %s "$f")
  if [ "$size" -eq 196992 ]; then
    rm -f -- "${f:?}"
    echo "$(date -u +%FT%TZ) removed $f bytes=$size (this run's staging copy of a prepared, never-used overlay file)" | tee -a "$E/host/removals.txt"
  fi
done
echo "overlay files left in this run's labs and staging copies: $(ls /var/tmp/cp-release-drill-s5-*/cells/*/*/overlay.qcow2 $R/ramstage/*/overlay.qcow2 2>/dev/null | wc -l)"
