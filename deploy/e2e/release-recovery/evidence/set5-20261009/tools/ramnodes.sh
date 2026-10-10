#!/bin/bash
# set5: after a lab was prepared and before its guests start, put each guest's node directory (the overlay disk, the
# seed image, QEMU's pid file, socket and serial log) on a RAM-backed tmpfs mount of this WSL host, in place.
# Why: the WSL virtual disk does not give space back to Windows C: when a file is deleted, the host's free-disk rule
# (40 GiB before every guest start) left 1.9 GiB when the cells began, and one cell writes about 2 GB of overlay.
# Nothing of the lab's plan changes (the paths are the same; QEMU's command line is the planned one; cache=none works
# on tmpfs with this kernel, probed in ramtest.sh). The lab's evidence directory and its plan stay on the disk.
# usage: ramnodes.sh /var/tmp/cp-release-drill-s5-NAME      (run by run-set5.sh through SET5_AFTER_PREPARE)
set -euo pipefail
root=$1
case $root in /var/tmp/cp-release-drill-s5-*) ;; *) echo "refusing: $root is not a lab of this run"; exit 2;; esac
[ -f "$root/lab.json" ] || { echo "refusing: $root is not a prepared lab"; exit 2; }
R=/var/tmp/cp-set5-run
lab=$(basename "$root")
avail=$(awk '/^MemAvailable:/ {print int($2 / 1024)}' /proc/meminfo)
other=$(findmnt -rn -t tmpfs -o TARGET | grep -c '^/var/tmp/cp-release-drill-' || true)
{
  echo "$(date -u +%FT%TZ) $lab: MemAvailable ${avail} MiB before the mounts; tmpfs mounts of other labs present: $other"
} >> $R/ram-nodes.txt
[ "$other" -eq 0 ] || { echo "refusing: a tmpfs mount of another lab is still present"; findmnt -rn -t tmpfs -o TARGET | grep '^/var/tmp/cp-release-drill-'; exit 3; }
[ "$avail" -ge 9000 ] || { echo "refusing: only ${avail} MiB of memory available on the WSL host"; exit 3; }
mkdir -p $R/ramstage
n=0
for overlay in "$root"/cells/*/*/overlay.qcow2; do
  [ -f "$overlay" ] || continue
  d=$(dirname "$overlay"); node=$(basename "$d")
  mountpoint -q "$d" && { echo "refusing: $d is already a mount point"; exit 3; }
  keep=$R/ramstage/$lab-$node-before
  [ -e "$keep" ] && { echo "refusing: $keep exists"; exit 3; }
  mkdir -m 700 "$keep"
  cp -a "$d/." "$keep/"
  before=$(cd "$d" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64)
  mode=$(stat -c %a "$d"); owner=$(stat -c %u:%g "$d")
  mount -t tmpfs -o size=8g,mode=0700,nosuid,nodev tmpfs "$d"
  chown "$owner" "$d"; chmod "$mode" "$d"
  cp -a "$keep/." "$d/"
  after=$(cd "$d" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64)
  [ "$before" = "$after" ] || { echo "the node directory's files differ after the move onto tmpfs"; exit 4; }
  n=$((n + 1))
  echo "$(date -u +%FT%TZ) $lab: $d is a tmpfs mount now (size limit 8g, mode $mode, owner $owner); its $(find "$d" -type f | wc -l) files are byte-equal to the prepared ones (digest of the listing $after)" >> $R/ram-nodes.txt
done
[ "$n" -ge 1 ] || { echo "no node directory with an overlay disk was found"; exit 4; }
findmnt -rn -t tmpfs -o TARGET,SIZE,USED | grep "^$root/" >> $R/ram-nodes.txt
findmnt -rn -t tmpfs -o TARGET,SIZE,USED | grep "^$root/" > "$root/ram-node-directories.txt"
echo "RAM-backed node directories of $lab: $n"
