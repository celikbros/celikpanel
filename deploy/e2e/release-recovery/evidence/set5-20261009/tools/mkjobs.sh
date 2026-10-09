#!/bin/bash
# set5: write the ten cell job files for run copy COPY and artifacts document ART, with run suffix SUFFIX (a, b, ...).
# usage: mkjobs.sh COPY ART SUFFIX [SHORTS]   -> job-cell-<short>-<suffix>.sh beside this script; prints the queue items
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
copy=$1; art=$2; sfx=$3
# the gate is reached through the run directory, so that a job file holds no path of the local scratch folder
printf '#!/bin/bash
exec bash %s/gate.sh "$@"
' "$J" > /var/tmp/cp-set5-run/gate.sh
printf '#!/bin/bash
exec bash %s/ramnodes.sh "$@"
' "$J" > /var/tmp/cp-set5-run/ramnodes.sh
n=0
case $sfx in a) base=4611;; b) base=4811;; c) base=5011;; *) echo "suffix a, b or c"; exit 2;; esac
only=${4:-}
while read -r short cell node; do
  port=$((base + 10 * n)); lport=$((port + 14000)); n=$((n + 1))
  lab=s5-$short-$sfx
  # an optional fourth argument names the cells to write (space-separated short names); the others are left as they are
  if [ -n "$only" ] && ! echo " $only " | grep -q " $short "; then continue; fi
  {
    echo '#!/bin/bash'
    echo 'export PYTHONDONTWRITEBYTECODE=1'
    echo "export SET5_DISK_GATE=/var/tmp/cp-set5-run/gate.sh"
    echo "export SET5_AFTER_PREPARE=/var/tmp/cp-set5-run/ramnodes.sh"
    echo "export CELIKPANEL_LAB_LINK_BASE_IMAGES=1"
    echo "exec bash /var/tmp/cp-set5-run/harness-$copy/deploy/e2e/release-recovery/run-set5.sh cell $cell $art $lab $port $lport"
  } > $J/job-cell-$short-$sfx.sh
  echo "cell-$short-$sfx:$lab:$node:$cell:run-$sfx"
done <<'LIST'
d13-good upd1-debian13-good debian13
ub-good upd1-ubuntu-good ubuntu
arch-good upd1-arch-good arch
d13-def upd1-debian13-defective debian13
ub-def upd1-ubuntu-defective ubuntu
arch-def upd1-arch-defective arch
d13-sc upd1-debian13-startcheck debian13
d13-oc upd1-debian13-owner-continuation debian13
ub-oc upd1-ubuntu-owner-continuation ubuntu
d13-mr upd1-debian13-mgmt-off-reboot debian13
LIST
