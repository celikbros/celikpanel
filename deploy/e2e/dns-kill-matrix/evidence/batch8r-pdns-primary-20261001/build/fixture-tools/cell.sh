#!/bin/bash
# usage: cell.sh SHORT CELL ROOT GATEPROBE(0|1) RUN_FLAGS...
# Fresh paired PowerDNS primary (Debian 13 kill guest) with the panel-free native BIND secondary (Arch).
# prepare .. start watcher; then runonly.sh (dry run and run-prepared --execute)
set -uo pipefail
SHORT=$1 CELL=$2 ROOT=$3 GATE=$4; shift 4; FLAGS=("$@")
NODE=debian13; ONODE=arch; FIX=uninitialized
LROOT=/var/tmp/cp-b8r-1001
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r
LOG=$LROOT/logs/$SHORT
test ! -e $LOG || { echo "log dir exists: $LOG"; exit 1; }
mkdir -p $LOG
exec >> $LOG/driver.log 2>&1
cd /root/cp-b8r-src
F=deploy/e2e/dns-kill-matrix/fixture.py
B=deploy/e2e/dns-kill-matrix/guest_bootstrap.py
PB=deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py
A=$ROOT/artifacts
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
O="python3 $SP/gssh.py $ROOT $CELL $ONODE"
COMMON="--work-root $ROOT --cell-id $CELL --node $NODE --identity-file $ROOT/id_ed25519 --source-fixture $FIX"
PCOMMON="--work-root $ROOT --cell-id $CELL --identity-file $ROOT/id_ed25519 --source-fixture $FIX"
step() { echo "### $(date -u +%FT%T.%3NZ) $*"; }
fail() { echo "### FAILED at $*"; echo "RUN_PREPARED_RC=not-run (failed at $*)" > $LOG/run-prepared.rc; exit 1; }
step start-cell $SHORT $CELL node=$NODE peer=$ONODE fixture=$FIX root=$ROOT gateprobe=$GATE flags="${FLAGS[*]}"
echo "$ROOT" > $LOG/work-root.txt
step prepare; python3 $F prepare --work-root $ROOT --cell-id $CELL --ssh-public-key $ROOT/id_ed25519.pub --execute > $LOG/prepare.json || fail prepare
step early-admission-dry-run
python3 $B run-prepared $COMMON "${FLAGS[@]}" > $LOG/run-prepared-dryrun-early.log 2>&1 < /dev/null || fail early-admission-dry-run
step start; python3 $F start --work-root $ROOT --cell-id $CELL --execute > $LOG/start.json || fail start
step wait-ssh; python3 $F wait-ssh --work-root $ROOT --cell-id $CELL --identity-file $ROOT/id_ed25519 --timeout 900 --execute > $LOG/wait-ssh.json || fail wait-ssh
step guest-identity
$G 'cat /etc/os-release; uname -a; echo "boot_id=$(cat /proc/sys/kernel/random/boot_id)"; echo "machine_id=$(cat /etc/machine-id)"; echo "product_uuid=$(sudo cat /sys/class/dmi/id/product_uuid)"; cat /etc/cloud/build.info 2>/dev/null; cat /etc/celikpanel-dns-kill-matrix; date -u' > $LOG/guest-identity.txt 2>&1 < /dev/null
$O 'cat /etc/os-release; uname -a; echo "boot_id=$(cat /proc/sys/kernel/random/boot_id)"; echo "machine_id=$(cat /etc/machine-id)"; echo "product_uuid=$(sudo cat /sys/class/dmi/id/product_uuid)"; cat /etc/celikpanel-dns-kill-matrix; date -u' > $LOG/peer-identity.txt 2>&1 < /dev/null
step install
python3 $B install $COMMON --agent $A/agent --tagged-agent $A/agent.kill --panel $A/panel --trigger $A/dns-kill-trigger --web-dir /root/cp-b8r-src/web/dist --execute > $LOG/install.log 2>&1 < /dev/null || fail install
step enroll-recovery-runtime
python3 $B enroll-recovery-runtime $COMMON --recovery-runtime $A/recovery-runtime --execute > $LOG/enroll-recovery-runtime.log 2>&1 < /dev/null || fail enroll-recovery-runtime
step native-bind-secondary-prepare
python3 $PB prepare $PCOMMON --execute > $LOG/peer-prepare.log 2>&1 < /dev/null || fail native-bind-secondary-prepare
step prepare-pdns-switch
python3 $B prepare-pdns-switch $COMMON --execute > $LOG/prepare-source.log 2>&1 < /dev/null || fail prepare-pdns-switch
step pre-run-snapshot
$G 'sudo bash -s' < $SP/unitfacts_remote.sh > $LOG/pre-run-units.txt 2>&1
$G 'sudo bash -s' < $SP/versions_remote.sh > $LOG/versions-pre-run-guest.txt 2>&1
$O 'sudo bash -s' < $SP/versions_remote.sh > $LOG/versions-pre-run-peer.txt 2>&1
$G 'sudo python3 - /var/lib/powerdns/pdns.sqlite3' < $SP/pdnsdb.py > $LOG/pdns-db-pre-run.txt 2>&1
$G 'sudo bash -s' < $SP/primstate_remote.sh > $LOG/primary-state-pre-run.txt 2>&1
$G 'sudo python3 -' < $SP/pairq.py > $LOG/dns-pre-run-guest.txt 2>&1
$O 'sudo python3 -' < $SP/pairq.py > $LOG/dns-pre-run-peer.txt 2>&1
$O 'sudo bash -s' < $SP/bindsec_remote.sh > $LOG/bind-secondary-state-pre-run.txt 2>&1
$O 'sudo bash -s bind' < $SP/peerfacts_remote.sh > $LOG/peer-facts-pre-run.txt 2>&1
if [ "$GATE" = 1 ]; then
  step gate-probe-on-prepared-guest
  $G 'sudo bash -s' < $SP/gateprobe_remote.sh > $LOG/gate-probe-prepared-guest.txt 2>&1
  grep -E '^GATE_PROBE_RC|"gate"' $LOG/gate-probe-prepared-guest.txt
  if grep -q '"gate":"closed"' $LOG/gate-probe-prepared-guest.txt; then
    echo "### GATE CLOSED IN THIS BUILD: stop"; echo "RUN_PREPARED_RC=not-run (gate closed on the prepared guest)" > $LOG/run-prepared.rc; exit 1
  fi
  if ! grep -q '"gate":"open"' $LOG/gate-probe-prepared-guest.txt; then
    echo "### GATE NOT ESTABLISHED: stop"; echo "RUN_PREPARED_RC=not-run (gate probe did not answer open)" > $LOG/run-prepared.rc; exit 1
  fi
fi
step start-peer-dns-sampler
$(python3 $SP/scpcmd.py $ROOT $CELL $ONODE) $SP/pairq.py celik@127.0.0.1:/tmp/cp-b8r-pairq.py < /dev/null || fail scp-pairq
$O 'sudo bash -s 1' < $SP/peerloop_start_remote.sh >> $LOG/driver.log 2>&1
step start-watcher
$(python3 $SP/scpcmd.py $ROOT $CELL $NODE) $SP/watcher.sh celik@127.0.0.1:/tmp/cp-b8r-watcher.sh < /dev/null || fail scp-watcher
$G "sudo install -m 0700 /tmp/cp-b8r-watcher.sh /root/cp-b8r-watcher.sh && sudo systemd-run --unit=cp-b8r-watch --collect /bin/bash /root/cp-b8r-watcher.sh $CELL /var/tmp/cp-b8r-watch" < /dev/null || fail start-watcher
step start-dbwatch
$(python3 $SP/scpcmd.py $ROOT $CELL $NODE) $SP/dbwatch.sh celik@127.0.0.1:/tmp/cp-b8r-dbwatch.sh < /dev/null || fail scp-dbwatch
$G "sudo install -m 0700 /tmp/cp-b8r-dbwatch.sh /root/cp-b8r-dbwatch.sh && sudo systemd-run --unit=cp-b8r-dbwatch --collect /bin/bash /root/cp-b8r-dbwatch.sh /var/tmp/cp-b8r-watch-db" < /dev/null || fail start-dbwatch
B0=$($G 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n')
P0=$($O 'cat /proc/sys/kernel/random/boot_id' < /dev/null | tr -d '\r\n')
echo "$(date -u +%FT%T.%NZ) boot 1 $B0 (watcher /var/tmp/cp-b8r-watch)" > $LOG/boot-monitor.log
echo "$(date -u +%FT%T.%NZ) peerboot 1 $P0 (sampler /var/tmp/cp-b8r-peerloop1)" >> $LOG/boot-monitor.log
step prepared-ok
bash $SP/runonly.sh "$SHORT" "$CELL" "$ROOT" "${FLAGS[@]}"
