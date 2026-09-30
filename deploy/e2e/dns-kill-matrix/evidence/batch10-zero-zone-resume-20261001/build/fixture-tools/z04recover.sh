#!/bin/bash
# Cell 1 (z04 resume), stage B: the new trigger (0d4c0324) in place of the batch 9 trigger on the primary guest
# (the harness runs /opt/celikpanel/bin/dns-kill-trigger on the guest over SSH; the product binaries are not touched),
# then the README's z04 resume command: zone-lifecycle --zero-zones --recover-delete (new harness from /root/cp-b10-src).
set -uo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b9-1001/r2; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b10-1001/logs/z04-resume
exec >> $LOG/driver.log 2>&1
cd /root/cp-b10-src
G="python3 $SP/gssh.py $ROOT $CELL debian13"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
COMMON="--work-root $ROOT --cell-id $CELL --node debian13 --identity-file $ROOT/id_ed25519 --source-fixture uninitialized"
step replace-trigger "(batch 9 3c60de67 -> batch 10 $(sha256sum /root/cp-b10-artifacts/dns-kill-trigger | cut -c1-8); same owner and mode root:root 0755)"
$(python3 $SP/scpcmd.py $ROOT $CELL debian13) /root/cp-b10-artifacts/dns-kill-trigger celik@127.0.0.1:/tmp/cp-b10-dns-kill-trigger < /dev/null || { echo "scp failed"; exit 1; }
$G 'sudo sha256sum /opt/celikpanel/bin/dns-kill-trigger && sudo install -o root -g root -m 0755 /tmp/cp-b10-dns-kill-trigger /opt/celikpanel/bin/dns-kill-trigger && sudo rm -f /tmp/cp-b10-dns-kill-trigger && sudo sha256sum /opt/celikpanel/bin/dns-kill-trigger && stat -c "%n %U:%G %a %s %y" /opt/celikpanel/bin/dns-kill-trigger' < /dev/null > $LOG/trigger-replacement.txt 2>&1
cat $LOG/trigger-replacement.txt
grep -q "$(sha256sum /root/cp-b10-artifacts/dns-kill-trigger | cut -c1-64)  /opt/celikpanel/bin/dns-kill-trigger" $LOG/trigger-replacement.txt || { echo "TRIGGER NOT REPLACED: stop"; exit 1; }
step pending-job-just-before "(read-only)"
$G 'sudo bash -s' < $SP/z04state_remote.sh > $LOG/primary-state-before-recover.txt 2>&1
CD=$ROOT/cells/7392457812ac089a866de6ec
ls -la --time-style=full-iso $CD/fresh-primary-peer
step zone-lifecycle-recover-dry-run
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete > $LOG/zone-lifecycle-recover-dryrun.log 2>&1 < /dev/null; echo "dry rc=$?"
step zone-lifecycle-recover-execute "python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete --execute"
T0=$(date +%s.%N)
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete --execute > $LOG/zone-lifecycle-recover.log 2>&1 < /dev/null
rc=$?
T1=$(date +%s.%N)
echo "ZONE_LIFECYCLE_RECOVER_RC=$rc elapsed=$(python3 -c "print(round($T1-$T0,2))")s" | tee $LOG/zone-lifecycle-recover.rc
ls -la --time-style=full-iso $CD/fresh-primary-peer
step after-recover "(read-only)"
$G 'sudo bash -s' < $SP/z04state_remote.sh > $LOG/primary-state-after-recover.txt 2>&1
step done
echo Z04-RECOVER-DONE
