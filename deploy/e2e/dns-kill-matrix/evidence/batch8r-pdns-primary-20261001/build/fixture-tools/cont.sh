#!/bin/bash
# usage: cont.sh SHORT CELL ROOT RUN_FLAGS...
# Continues a prepared cell after cell.sh stopped at its own standalone gate-probe step (no mutation happened):
# starts the peer DNS sampler and the guest watcher exactly as cell.sh does, then runonly.sh, then collect.sh.
set -uo pipefail
SHORT=$1 CELL=$2 ROOT=$3; shift 3; FLAGS=("$@")
NODE=debian13; ONODE=arch
LROOT=/var/tmp/cp-b8r-1001
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r
LOG=$LROOT/logs/$SHORT
test -e $LOG/run-prepared.rc && mv $LOG/run-prepared.rc $LOG/run-prepared.rc.standalone-gate-probe-attempt
(
exec >> $LOG/driver.log 2>&1
cd /root/cp-b8r-src
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
O="python3 $SP/gssh.py $ROOT $CELL $ONODE"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
fail() { echo "### FAILED at $*"; echo "RUN_PREPARED_RC=not-run (failed at $*)" > $LOG/run-prepared.rc; exit 1; }
step continue-after-standalone-gate-probe-attempt
step start-peer-dns-sampler
$(python3 $SP/scpcmd.py $ROOT $CELL $ONODE) $SP/pairq.py celik@127.0.0.1:/tmp/cp-b8r-pairq.py < /dev/null || fail scp-pairq
$O 'sudo bash -s 1' < $SP/peerloop_start_remote.sh >> $LOG/driver.log 2>&1
step start-watcher
$(python3 $SP/scpcmd.py $ROOT $CELL $NODE) $SP/watcher.sh celik@127.0.0.1:/tmp/cp-b8r-watcher.sh < /dev/null || fail scp-watcher
$G "sudo install -m 0700 /tmp/cp-b8r-watcher.sh /root/cp-b8r-watcher.sh && sudo systemd-run --unit=cp-b8r-watch --collect /bin/bash /root/cp-b8r-watcher.sh $CELL /var/tmp/cp-b8r-watch" < /dev/null || fail start-watcher
B0=$($G 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n')
P0=$($O 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n')
echo "$(date -u +%FT%T.%NZ) boot 1 $B0 (watcher /var/tmp/cp-b8r-watch)" > $LOG/boot-monitor.log
echo "$(date -u +%FT%T.%NZ) peerboot 1 $P0 (sampler /var/tmp/cp-b8r-peerloop1)" >> $LOG/boot-monitor.log
step prepared-ok
)
if grep -q 'not-run' $LOG/run-prepared.rc 2>/dev/null; then echo "NO-COLLECT" >> $LOG/driver.log; exit 1; fi
bash $SP/runonly.sh "$SHORT" "$CELL" "$ROOT" "${FLAGS[@]}"
if grep -q 'not-run' $LOG/run-prepared.rc 2>/dev/null; then
  echo "NO-COLLECT (dry run failed)" >> $LOG/driver.log
else
  bash $SP/collect.sh "$SHORT" "$CELL" "$ROOT"
fi
echo "ALLDONE $(date -u +%FT%TZ)" >> $LOG/driver.log
