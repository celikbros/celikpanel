#!/bin/bash
# usage: runonly.sh SHORT CELL FIXTURE ROOT NODE PEER FORMAT RUN_FLAGS...   (dry run, then run-prepared --execute)
set -uo pipefail
SHORT=$1 CELL=$2 FIX=$3 ROOT=$4 NODE=$5 PEER=$6 FORMAT=$7; shift 7; FLAGS=("$@")
LROOT=/var/tmp/cp-b6b-0930
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch6b
LOG=$LROOT/logs/$SHORT
exec >> $LOG/driver.log 2>&1
cd /root/cp-b6b-src
B=deploy/e2e/dns-kill-matrix/guest_bootstrap.py
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
COMMON="--work-root $ROOT --cell-id $CELL --node $NODE --identity-file $ROOT/id_ed25519 --source-fixture $FIX"
PEERARG=()
[ "$PEER" != none ] && PEERARG=(--peer-engine "$PEER")
[ "$FORMAT" != none ] && PEERARG+=(--peer-catalog-format "$FORMAT")
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
step run-prepared-dry-run flags="${FLAGS[*]}"
if ! python3 $B run-prepared $COMMON "${PEERARG[@]}" "${FLAGS[@]}" > $LOG/run-prepared-dryrun.log 2>&1 < /dev/null; then
  echo "### FAILED at run-prepared-dry-run"; echo "RUN_PREPARED_RC=not-run (failed at run-prepared-dry-run)" > $LOG/run-prepared.rc; exit 1
fi
B0=$(grep -o 'boot 1 [0-9a-f-]*' $LOG/boot-monitor.log | head -1 | cut -d' ' -f3)
# Host-side boot monitor: after each guest reboot, start a fresh read-only watcher in a new directory.
(
  known="$B0"; n=1
  while [ ! -e $LOG/run-prepared.rc ]; do
    b=$(timeout 15 $G 'cat /proc/sys/kernel/random/boot_id' < /dev/null 2>/dev/null | tr -d '\r\n')
    if [ ${#b} -eq 36 ] && [[ " $known " != *" $b "* ]]; then
      known="$known $b"; n=$((n+1))
      echo "$(date -u +%FT%T.%NZ) boot $n $b (watcher /var/tmp/cp-b6b-watch-boot$n)" >> $LOG/boot-monitor.log
      timeout 30 $G "sudo systemd-run --unit=cp-b6b-watch-boot$n --collect /bin/bash /root/cp-b6b-watcher.sh $CELL /var/tmp/cp-b6b-watch-boot$n" < /dev/null >> $LOG/boot-monitor.log 2>&1
      echo "watcher start rc=$?" >> $LOG/boot-monitor.log
    fi
    sleep 3
  done
) &
MON=$!
step run-prepared
python3 $B run-prepared $COMMON "${PEERARG[@]}" "${FLAGS[@]}" --execute > $LOG/run-prepared.log 2>&1 < /dev/null
rc=$?
echo "RUN_PREPARED_RC=$rc" | tee $LOG/run-prepared.rc
wait $MON
step done-run rc=$rc
