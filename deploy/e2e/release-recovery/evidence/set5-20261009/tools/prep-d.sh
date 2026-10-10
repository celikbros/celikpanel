#!/bin/bash
# set5: dry runs on copy d, then the job files of run suffix a
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set5-run
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ART=$(cat $R/artifacts.path)
H=$R/harness-d/deploy/e2e/release-recovery
for c in upd1-debian13-good upd1-ubuntu-good upd1-arch-good upd1-debian13-defective upd1-ubuntu-defective upd1-arch-defective upd1-debian13-startcheck upd1-debian13-owner-continuation upd1-ubuntu-owner-continuation upd1-debian13-mgmt-off-reboot; do
  bash $H/run-set5.sh dry-run $c "$ART" dry-$c > $R/build/dry-d-$c.json 2> $R/build/dry-d-$c.stderr.txt; echo "dry $c rc=$?"
done
bash $J/mkjobs.sh d "$ART" a
cat $J/job-cell-d13-good-a.sh
cat $R/gate.sh $R/ramnodes.sh | grep -c exec
