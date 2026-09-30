#!/bin/bash
# usage: recover12.sh SHORT CELL ROOT -- z04 after the owner's enrollment: the README resume command
#   guest_bootstrap.py zone-lifecycle COMMON --zero-zones --recover-delete (dry run, then --execute), with read-only state before/after.
set -uo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
SHORT=$1 CELL=$2 ROOT=$3
LOG=/var/tmp/cp-b12-1001/logs/$SHORT
test ! -e $LOG/zone-lifecycle-recover.rc || { echo "recover already run"; exit 1; }
exec >> $LOG/driver.log 2>&1
cd /root/cp-b12-src
G="python3 $SP/gssh.py $ROOT $CELL debian13"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
COMMON="--work-root $ROOT --cell-id $CELL --node debian13 --identity-file $ROOT/id_ed25519 --source-fixture uninitialized"
CD=$(python3 -c 'import sys;sys.path.insert(0,"/root/cp-b12-src/deploy/e2e/dns-kill-matrix");import fixture;from pathlib import Path;print(fixture.load_cell_plan(Path(sys.argv[1]).resolve(),sys.argv[2])["cell_directory"])' $ROOT $CELL)
step pending-job-just-before "(read-only)"
$G 'sudo bash -s' < $SP/state_remote.sh > $LOG/primary-state-before-recover.txt 2>&1
bash $SP/rndcfacts.sh $ROOT $CELL $LOG/secondary-rndc-prerequisite-before-recover.txt > /dev/null
ls -la --time-style=full-iso $CD/fresh-primary-peer
step zone-lifecycle-recover-dry-run
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete > $LOG/zone-lifecycle-recover-dryrun.log 2>&1 < /dev/null; echo "dry rc=$?"
step zone-lifecycle-recover-execute "python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete --execute"
T0=$(date +%s.%N)
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete --execute > $LOG/zone-lifecycle-recover.log 2>&1 < /dev/null
rc=$?
T1=$(date +%s.%N)
echo "ZONE_LIFECYCLE_RECOVER_RC=$rc elapsed=$(python3 -c "print(round($T1-$T0,2))")s start=$(date -u -d @$T0 +%FT%T.%3NZ) end=$(date -u -d @$T1 +%FT%T.%3NZ)" | tee $LOG/zone-lifecycle-recover.rc
ls -la --time-style=full-iso $CD/fresh-primary-peer
step after-recover "(read-only)"
$G 'sudo bash -s' < $SP/state_remote.sh > $LOG/primary-state-after-recover.txt 2>&1
step done
echo RECOVER-DONE
