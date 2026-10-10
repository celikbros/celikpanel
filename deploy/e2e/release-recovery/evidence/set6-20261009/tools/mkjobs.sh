#!/bin/bash
# set6: write the six cell job files for run copy COPY, with run suffix SUFFIX (a, b, ...).
# usage: mkjobs.sh COPY SUFFIX [SHORTS]   -> job-cell-<short>-<suffix>.sh beside this script; prints the queue items
# The three fresh-install cells read the artifacts document of build `cur` (its baseline is the candidate), the
# three update cells the one of build `a81` (its baseline is the published v0.1.0-alpha.81).
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set6-run
copy=$1; sfx=$2
# the gate is reached through the run directory, so that a job file holds no path of the local scratch folder
printf '#!/bin/bash\nexec bash %s/gate.sh "$@"\n' "$J" > $R/gate.sh
n=0
case $sfx in a) base=4611;; b) base=4811;; c) base=5011;; d) base=5211;; *) echo "suffix a, b, c or d"; exit 2;; esac
only=${3:-}
while read -r short cell node build; do
  port=$((base + 10 * n)); lport=$((port + 14000)); n=$((n + 1))
  lab=s6-$short-$sfx
  if [ -n "$only" ] && ! echo " $only " | grep -q " $short "; then continue; fi
  art=$(cat $R/artifacts-$build.path)
  {
    echo '#!/bin/bash'
    echo 'export PYTHONDONTWRITEBYTECODE=1'
    echo "export SET6_DISK_GATE=$R/gate.sh"
    echo "export CELIKPANEL_LAB_LINK_BASE_IMAGES=1"
    echo "exec bash $R/harness-$copy/deploy/e2e/release-recovery/run-set6.sh cell $cell $art $lab $port $lport"
  } > $J/job-cell-$short-$sfx.sh
  echo "cell-$short-$sfx:$lab:$node:$cell:run-$sfx"
done <<'LIST'
arch-fresh set6-arch arch cur
d13-fresh set6-debian13 debian13 cur
ub-fresh set6-ubuntu ubuntu cur
arch-good upd1-arch-good arch a81
d13-good upd1-debian13-good debian13 a81
ub-good upd1-ubuntu-good ubuntu a81
LIST
