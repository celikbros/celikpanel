#!/bin/bash
# Cell 1 (z04 resume), stage A: boot the kept batch 9 z04 overlays (no reinstall, no re-prepare), read-only state, samplers.
set -uo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b9-1001/r2; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
CD=$ROOT/cells/7392457812ac089a866de6ec
LROOT=/var/tmp/cp-b10-1001
LOG=$LROOT/logs/z04-resume
test ! -e $LOG || { echo "log dir exists: $LOG"; exit 1; }
mkdir -p $LOG/pre-boot
exec >> $LOG/driver.log 2>&1
cd /root/cp-b10-src
F=deploy/e2e/dns-kill-matrix/fixture.py
G="python3 $SP/gssh.py $ROOT $CELL debian13"
O="python3 $SP/gssh.py $ROOT $CELL arch"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
step z04-boot start root=$ROOT cell=$CELL
if pgrep -x qemu-system-x86_64 >/dev/null; then echo "QEMU already running: stop"; exit 1; fi
step pre-boot record "(read-only; serial logs copied because QEMU truncates them at start)"
ls -la --time-style=full-iso $CD $CD/debian13 $CD/arch $CD/fresh-primary-peer > $LOG/pre-boot/cell-dir-ls.txt 2>&1
sha256sum $CD/fixture-plan.json $CD/ssh-known-hosts $CD/fresh-primary-peer/* > $LOG/pre-boot/cell-files.sha256
cp -p $CD/debian13/serial.log $LOG/pre-boot/serial-debian13-batch9.log
cp -p $CD/arch/serial.log $LOG/pre-boot/serial-arch-batch9.log
for n in debian13 arch; do qemu-img info -U $CD/$n/overlay.qcow2 > $LOG/pre-boot/qemu-img-info-$n.txt 2>&1; done
step fixture-start "(QEMU from the batch 9 cell plan; new harness fixture.py, unchanged since 3cceb29a)"
python3 $F start --work-root $ROOT --cell-id $CELL --execute > $LOG/start.json 2>&1 || { echo "START FAILED"; cat $LOG/start.json; exit 1; }
step wait-ssh
python3 $F wait-ssh --work-root $ROOT --cell-id $CELL --identity-file $ROOT/id_ed25519 --timeout 900 --execute > $LOG/wait-ssh.json 2>&1 || { echo "WAIT-SSH FAILED"; cat $LOG/wait-ssh.json; exit 1; }
step first-read "(read-only: ledger, agent, journal this boot)"
$G 'sudo bash -s' < $SP/z04state_remote.sh > $LOG/primary-state-after-boot.txt 2>&1
$O 'echo "boot_id=$(cat /proc/sys/kernel/random/boot_id)"; uname -r; date -u +%FT%T.%NZ; systemctl show named.service -p ActiveState,SubState,MainPID,ActiveEnterTimestamp --value; sudo ls -la --time-style=full-iso /root/dns-owner-tools /root/celikpanel-primary-inspector.pub; sudo /root/dns-owner-tools/dns-peer-enroll secondary-status' > $LOG/secondary-state-after-boot.txt 2>&1 < /dev/null
step start-samplers
$(python3 $SP/scpcmd.py $ROOT $CELL arch) $SP/pairq.py celik@127.0.0.1:/tmp/cp-b10-pairq.py < /dev/null
$O 'sudo bash -s 1' < $SP/peerloop_start_remote.sh
$(python3 $SP/scpcmd.py $ROOT $CELL debian13) $SP/dbwatch.sh $SP/ledwatch.sh celik@127.0.0.1:/tmp/ < /dev/null
$G "sudo install -m 0700 /tmp/dbwatch.sh /root/cp-b10-dbwatch.sh && sudo install -m 0700 /tmp/ledwatch.sh /root/cp-b10-ledwatch.sh && sudo systemd-run --unit=cp-b10-dbwatch --collect /bin/bash /root/cp-b10-dbwatch.sh /var/tmp/cp-b10-watch-db && sudo systemd-run --unit=cp-b10-ledwatch --collect /bin/bash /root/cp-b10-ledwatch.sh /var/tmp/cp-b10-watch-led" < /dev/null
B0=$($G 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n'); P0=$($O 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n')
echo "$(date -u +%FT%T.%NZ) boot 1 $B0 (dbwatch /var/tmp/cp-b10-watch-db, ledwatch /var/tmp/cp-b10-watch-led)" > $LOG/boot-monitor.log
echo "$(date -u +%FT%T.%NZ) peerboot 1 $P0 (sampler /var/tmp/cp-b10-peerloop1)" >> $LOG/boot-monitor.log
step z04-boot done
echo Z04-BOOT-DONE
