#!/bin/bash
# set5: dry runs on copy e, then the job files of the cells that have not started yet (run suffix a)
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set5-run
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ART=$(cat $R/artifacts.path)
H=$R/harness-e/deploy/e2e/release-recovery
todo=""
for s in d13-def arch-good ub-def arch-def d13-sc d13-oc ub-oc d13-mr; do
  [ -e $R/logs/cell-$s-a.start ] && { echo "cell-$s-a has started already: its job file is left as it is"; continue; }
  todo="$todo $s"
done
for c in upd1-arch-good upd1-debian13-defective upd1-ubuntu-defective upd1-arch-defective upd1-debian13-startcheck upd1-debian13-owner-continuation upd1-ubuntu-owner-continuation upd1-debian13-mgmt-off-reboot; do
  bash $H/run-set5.sh dry-run $c "$ART" dry-$c > $R/build/dry-e-$c.json 2> $R/build/dry-e-$c.stderr.txt; echo "dry $c rc=$?"
done
bash $J/mkjobs.sh e "$ART" a "$todo" > /dev/null
grep -l 'harness-e' $J/job-cell-*-a.sh | xargs -n1 basename | tr '\n' ' '; echo
grep -l 'harness-d' $J/job-cell-*-a.sh | xargs -n1 basename | tr '\n' ' '; echo
echo "$(date -u +%FT%TZ) job files of the cells not yet started now name run copy e:$todo" >> $R/progress.txt
