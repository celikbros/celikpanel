#!/bin/bash
# usage: runonly.sh SHORT CELL ROOT RUN_FLAGS...   (dry run, then run-prepared --execute)
set -uo pipefail
SHORT=$1 CELL=$2 ROOT=$3; shift 3; FLAGS=("$@")
NODE=debian13; ONODE=arch; FIX=uninitialized
LROOT=/var/tmp/cp-b11-1001
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
LOG=$LROOT/logs/$SHORT
exec >> $LOG/driver.log 2>&1
cd /root/cp-b11-src
B=deploy/e2e/dns-kill-matrix/guest_bootstrap.py
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
O="python3 $SP/gssh.py $ROOT $CELL $ONODE"
COMMON="--work-root $ROOT --cell-id $CELL --node $NODE --identity-file $ROOT/id_ed25519 --source-fixture $FIX"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
step run-prepared-dry-run flags="${FLAGS[*]}"
if ! python3 $B run-prepared $COMMON "${FLAGS[@]}" > $LOG/run-prepared-dryrun.log 2>&1 < /dev/null; then
  echo "### FAILED at run-prepared-dry-run"; echo "RUN_PREPARED_RC=not-run (failed at run-prepared-dry-run)" > $LOG/run-prepared.rc; exit 1
fi
B0=$(grep -o 'boot 1 [0-9a-f-]*' $LOG/boot-monitor.log | head -1 | cut -d' ' -f3)
P0=$(grep -o 'peerboot 1 [0-9a-f-]*' $LOG/boot-monitor.log | head -1 | cut -d' ' -f3)
# Host-side boot monitor: after each reboot, start a fresh read-only watcher (primary) or DNS sampler (secondary) in a new directory.
(
  known="$B0"; n=1; pknown="$P0"; pn=1
  while [ ! -e $LOG/run-prepared.rc ]; do
    b=$(timeout 15 $G 'cat /proc/sys/kernel/random/boot_id' < /dev/null 2>/dev/null | tr -d '\r\n')
    if [ ${#b} -eq 36 ] && [[ " $known " != *" $b "* ]]; then
      known="$known $b"; n=$((n+1))
      echo "$(date -u +%FT%T.%NZ) boot $n $b (watcher /var/tmp/cp-b11-watch-boot$n)" >> $LOG/boot-monitor.log
      timeout 30 $G "sudo systemd-run --unit=cp-b11-watch-boot$n --collect /bin/bash /root/cp-b11-watcher.sh $CELL /var/tmp/cp-b11-watch-boot$n" < /dev/null >> $LOG/boot-monitor.log 2>&1
      echo "watcher start rc=$?" >> $LOG/boot-monitor.log
      timeout 30 $G "sudo systemd-run --unit=cp-b11-dbwatch-boot$n --collect /bin/bash /root/cp-b11-dbwatch.sh /var/tmp/cp-b11-watch-dbboot$n" < /dev/null >> $LOG/boot-monitor.log 2>&1
      echo "dbwatch start rc=$?" >> $LOG/boot-monitor.log
    fi
    p=$(timeout 15 $O 'cat /proc/sys/kernel/random/boot_id' < /dev/null 2>/dev/null | tr -d '\r\n')
    if [ ${#p} -eq 36 ] && [[ " $pknown " != *" $p "* ]]; then
      pknown="$pknown $p"; pn=$((pn+1))
      echo "$(date -u +%FT%T.%NZ) peerboot $pn $p (sampler /var/tmp/cp-b11-peerloop$pn)" >> $LOG/boot-monitor.log
      timeout 30 $O "sudo bash -s $pn" < $SP/peerloop_start_remote.sh >> $LOG/boot-monitor.log 2>&1
      echo "sampler start rc=$?" >> $LOG/boot-monitor.log
    fi
    sleep 3
  done
) &
MON=$!
step run-prepared
python3 $B run-prepared $COMMON "${FLAGS[@]}" --execute > $LOG/run-prepared.log 2>&1 < /dev/null
rc=$?
echo "RUN_PREPARED_RC=$rc" | tee $LOG/run-prepared.rc
wait $MON
step done-run rc=$rc
