#!/bin/bash
# usage: cell.sh SHORT CELL FIXTURE PREP ENROLL(0|1) ROOT NODE(debian13|arch) PEER(none|bind|pdns) FORMAT(none|bind|pdns-native) RUN_FLAGS...
# prepare .. start watcher; then runonly.sh (dry run and run-prepared --execute)
set -uo pipefail
SHORT=$1 CELL=$2 FIX=$3 PREP=$4 ENROLL=$5 ROOT=$6 NODE=$7 PEER=$8 FORMAT=$9; shift 9; FLAGS=("$@")
if [ "$NODE" = debian13 ]; then ONODE=arch; else ONODE=debian13; fi
LROOT=/var/tmp/cp-b6b-0930
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch6b
LOG=$LROOT/logs/$SHORT
mkdir -p $LOG
exec >> $LOG/driver.log 2>&1
cd /root/cp-b6b-src
F=deploy/e2e/dns-kill-matrix/fixture.py
B=deploy/e2e/dns-kill-matrix/guest_bootstrap.py
A=$ROOT/artifacts
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
O="python3 $SP/gssh.py $ROOT $CELL $ONODE"
COMMON="--work-root $ROOT --cell-id $CELL --node $NODE --identity-file $ROOT/id_ed25519 --source-fixture $FIX"
PEERARG=()
[ "$PEER" != none ] && PEERARG=(--peer-engine "$PEER")
[ "$FORMAT" != none ] && PEERARG+=(--peer-catalog-format "$FORMAT")
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
fail() { echo "### FAILED at $*"; echo "RUN_PREPARED_RC=not-run (failed at $*)" > $LOG/run-prepared.rc; exit 1; }
step start-cell $SHORT $CELL node=$NODE other=$ONODE fixture=$FIX prep=$PREP enroll=$ENROLL root=$ROOT peer=$PEER format=$FORMAT flags="${FLAGS[*]}"
echo "$ROOT" > $LOG/work-root.txt
step prepare; python3 $F prepare --work-root $ROOT --cell-id $CELL --ssh-public-key $ROOT/id_ed25519.pub --execute > $LOG/prepare.json || fail prepare
step early-admission-dry-run
python3 $B run-prepared $COMMON "${PEERARG[@]}" "${FLAGS[@]}" > $LOG/run-prepared-dryrun-early.log 2>&1 < /dev/null || fail early-admission-dry-run
step start; python3 $F start --work-root $ROOT --cell-id $CELL --execute > $LOG/start.json || fail start
step wait-ssh; python3 $F wait-ssh --work-root $ROOT --cell-id $CELL --identity-file $ROOT/id_ed25519 --timeout 900 --execute > $LOG/wait-ssh.json || fail wait-ssh
step guest-identity
$G 'cat /etc/os-release; uname -a; echo "boot_id=$(cat /proc/sys/kernel/random/boot_id)"; echo "machine_id=$(cat /etc/machine-id)"; echo "product_uuid=$(sudo cat /sys/class/dmi/id/product_uuid)"; cat /etc/cloud/build.info 2>/dev/null; cat /etc/celikpanel-dns-kill-matrix; date -u' > $LOG/guest-identity.txt 2>&1 < /dev/null
$O 'cat /etc/os-release; uname -a; echo "boot_id=$(cat /proc/sys/kernel/random/boot_id)"; echo "machine_id=$(cat /etc/machine-id)"; echo "product_uuid=$(sudo cat /sys/class/dmi/id/product_uuid)"; cat /etc/celikpanel-dns-kill-matrix; date -u' > $LOG/peer-identity.txt 2>&1 < /dev/null
step install
python3 $B install $COMMON --agent $A/agent --tagged-agent $A/agent.kill --panel $A/panel --trigger $A/dns-kill-trigger --web-dir /root/cp-b6b-src/web/dist --execute > $LOG/install.log 2>&1 < /dev/null || fail install
if [ "$ENROLL" = 1 ]; then
  step enroll-recovery-runtime
  python3 $B enroll-recovery-runtime $COMMON --recovery-runtime $A/recovery-runtime --execute > $LOG/enroll-recovery-runtime.log 2>&1 < /dev/null || fail enroll-recovery-runtime
fi
step $PREP
python3 $B $PREP $COMMON "${PEERARG[@]}" --execute > $LOG/prepare-source.log 2>&1 < /dev/null || fail $PREP
step pre-run-snapshot
$G 'sudo bash -s' < $SP/unitfacts_remote.sh > $LOG/pre-run-units.txt 2>&1
$G 'sudo bash -s' < $SP/versions_remote.sh > $LOG/versions-pre-run-guest.txt 2>&1
$O 'sudo bash -s' < $SP/versions_remote.sh > $LOG/versions-pre-run-other.txt 2>&1
if [ "$PEER" != none ]; then
  $G 'sudo python3 -' < $SP/pairq.py > $LOG/dns-pre-run-guest.txt 2>&1
  $O 'sudo python3 -' < $SP/pairq.py > $LOG/dns-pre-run-peer.txt 2>&1
  $G 'sudo bash -s' < $SP/secstate_remote.sh > $LOG/secondary-state-pre-run.txt 2>&1
  $O "sudo bash -s $PEER" < $SP/peerfacts_remote.sh > $LOG/peer-facts-pre-run.txt 2>&1
  step start-peer-dns-sampler
  $(python3 $SP/scpcmd.py $ROOT $CELL $ONODE) $SP/pairq.py celik@127.0.0.1:/tmp/cp-b6b-pairq.py < /dev/null || fail scp-pairq
  $O 'sudo bash -s' < $SP/peerloop_start_remote.sh >> $LOG/driver.log 2>&1
else
  $G 'sudo python3 -' < $SP/dnsq.py > $LOG/dns-pre-run.txt 2>&1
  $G 'sudo python3 -' < $SP/soaq.py > $LOG/soa-pre-run.txt 2>&1
fi
step start-watcher
$(python3 $SP/scpcmd.py $ROOT $CELL $NODE) $SP/watcher.sh celik@127.0.0.1:/tmp/cp-b6b-watcher.sh < /dev/null || fail scp-watcher
$G "sudo install -m 0700 /tmp/cp-b6b-watcher.sh /root/cp-b6b-watcher.sh && sudo systemd-run --unit=cp-b6b-watch --collect /bin/bash /root/cp-b6b-watcher.sh $CELL /var/tmp/cp-b6b-watch" < /dev/null || fail start-watcher
B0=$($G 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n')
echo "$(date -u +%FT%T.%NZ) boot 1 $B0 (watcher /var/tmp/cp-b6b-watch)" > $LOG/boot-monitor.log
step prepared-ok
bash $SP/runonly.sh "$SHORT" "$CELL" "$FIX" "$ROOT" "$NODE" "$PEER" "$FORMAT" "${FLAGS[@]}"
