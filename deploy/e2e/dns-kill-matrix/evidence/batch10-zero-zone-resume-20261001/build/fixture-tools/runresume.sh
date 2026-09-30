#!/bin/bash
# usage: runresume.sh SHORT CELL ROOT RUN_FLAGS...   (batch 10: dry run, then run-prepared RUN_FLAGS --resume-held-zone-lifecycle --execute)
# Same host-side boot monitor as runonly.sh (new watcher/dbwatch after a primary reboot, new DNS sampler after a secondary reboot), boot ordinals continued.
set -uo pipefail
SHORT=$1 CELL=$2 ROOT=$3; shift 3; FLAGS=("$@")
NODE=debian13; ONODE=arch; FIX=uninitialized
LROOT=/var/tmp/cp-b10-1001
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
LOG=$LROOT/logs/$SHORT
test ! -e $LOG/resume.rc || { echo "resume.rc exists"; exit 1; }
exec >> $LOG/driver.log 2>&1
cd /root/cp-b10-src
B=deploy/e2e/dns-kill-matrix/guest_bootstrap.py
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
O="python3 $SP/gssh.py $ROOT $CELL $ONODE"
COMMON="--work-root $ROOT --cell-id $CELL --node $NODE --identity-file $ROOT/id_ed25519 --source-fixture $FIX"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
step resume-dry-run flags="${FLAGS[*]} --resume-held-zone-lifecycle"
python3 $B run-prepared $COMMON "${FLAGS[@]}" --resume-held-zone-lifecycle > $LOG/resume-dryrun.log 2>&1 < /dev/null
echo "resume dry rc=$?"
known=$(grep -oE '^[^ ]+ boot [0-9]+ [0-9a-f-]+' $LOG/boot-monitor.log | awk '{print $4}' | tr '\n' ' ')
n=$(grep -cE '^[^ ]+ boot [0-9]+ ' $LOG/boot-monitor.log)
pknown=$(grep -oE '^[^ ]+ peerboot [0-9]+ [0-9a-f-]+' $LOG/boot-monitor.log | awk '{print $4}' | tr '\n' ' ')
pn=$(grep -cE '^[^ ]+ peerboot [0-9]+ ' $LOG/boot-monitor.log)
echo "boot monitor continues: primary boots $n ($known), secondary boots $pn ($pknown)"
(
  while [ ! -e $LOG/resume.rc ]; do
    b=$(timeout 15 $G 'cat /proc/sys/kernel/random/boot_id' < /dev/null 2>/dev/null | tr -d '\r\n')
    if [ ${#b} -eq 36 ] && [[ " $known " != *" $b "* ]]; then
      known="$known $b"; n=$((n+1))
      echo "$(date -u +%FT%T.%NZ) boot $n $b (watcher /var/tmp/cp-b10-watch-boot$n)" >> $LOG/boot-monitor.log
      timeout 30 $G "sudo systemd-run --unit=cp-b10-watch-boot$n --collect /bin/bash /root/cp-b10-watcher.sh $CELL /var/tmp/cp-b10-watch-boot$n" < /dev/null >> $LOG/boot-monitor.log 2>&1
      echo "watcher start rc=$?" >> $LOG/boot-monitor.log
      timeout 30 $G "sudo systemd-run --unit=cp-b10-dbwatch-boot$n --collect /bin/bash /root/cp-b10-dbwatch.sh /var/tmp/cp-b10-watch-dbboot$n" < /dev/null >> $LOG/boot-monitor.log 2>&1
      echo "dbwatch start rc=$?" >> $LOG/boot-monitor.log
    fi
    p=$(timeout 15 $O 'cat /proc/sys/kernel/random/boot_id' < /dev/null 2>/dev/null | tr -d '\r\n')
    if [ ${#p} -eq 36 ] && [[ " $pknown " != *" $p "* ]]; then
      pknown="$pknown $p"; pn=$((pn+1))
      echo "$(date -u +%FT%T.%NZ) peerboot $pn $p (sampler /var/tmp/cp-b10-peerloop$pn)" >> $LOG/boot-monitor.log
      timeout 30 $O "sudo bash -s $pn" < $SP/peerloop_start_remote.sh >> $LOG/boot-monitor.log 2>&1
      echo "sampler start rc=$?" >> $LOG/boot-monitor.log
    fi
    sleep 3
  done
) &
MON=$!
step run-prepared-resume "python3 $B run-prepared $COMMON ${FLAGS[*]} --resume-held-zone-lifecycle --execute"
T0=$(date +%s.%N)
python3 $B run-prepared $COMMON "${FLAGS[@]}" --resume-held-zone-lifecycle --execute > $LOG/resume.log 2>&1 < /dev/null
rc=$?
T1=$(date +%s.%N)
echo "RESUME_RC=$rc elapsed=$(python3 -c "print(round($T1-$T0,2))")s" | tee $LOG/resume.rc
wait $MON
step done-resume rc=$rc
