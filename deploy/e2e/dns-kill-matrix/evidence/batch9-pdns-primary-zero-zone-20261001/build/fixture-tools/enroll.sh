#!/bin/bash
# usage: enroll.sh SHORT CELL ROOT
# Owner enrollment of the native BIND secondary for the Agent's pending parentless deletion (README: dns-peer-enroll --engine bind
# on both guests, as the Agent's message names), then zone-lifecycle --zero-zones --recover-delete. Every command and its output
# is recorded in logs/SHORT/enroll.log. Owner tools: packaged build from 3cceb29a (ROOT/artifacts/dns-owner-tools).
set -uo pipefail
SHORT=$1 CELL=$2 ROOT=$3
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
LOG=/var/tmp/cp-b9-1001/logs/$SHORT
EL=$LOG/enroll.log
exec >> $EL 2>&1
cd /root/cp-b9-src
G="python3 $SP/gssh.py $ROOT $CELL debian13"
O="python3 $SP/gssh.py $ROOT $CELL arch"
SCPG=$(python3 $SP/scpcmd.py $ROOT $CELL debian13)
SCPO=$(python3 $SP/scpcmd.py $ROOT $CELL arch)
OT=$ROOT/artifacts/dns-owner-tools
CAT=catalog-c000020a.celikpanel.invalid
PIP=192.0.2.10; SIP=192.0.2.11
step() { echo; echo "### $(date -u +%FT%T.%3NZ) $*"; }
run() { local who=$1; shift; echo "\$ [$who] $*"; if [ $who = primary ]; then $G "$*" < /dev/null; else $O "$*" < /dev/null; fi; echo "[rc=$?]"; }
step owner-enrollment start cell=$CELL root=$ROOT
sha256sum $OT/dns-peer-enroll $OT/bind-peer-inspect
step restart read-only samplers after the first collect "(primary database copy sampler, secondary DNS sampler)"
run primary 'sudo rm -f /var/tmp/cp-b9-dbwatch.stop && sudo systemd-run --unit=cp-b9-dbwatch-enr --collect /bin/bash /root/cp-b9-dbwatch.sh /var/tmp/cp-b9-watch-dbenr'
echo "\$ [secondary] sudo bash -s enr < peerloop_start_remote.sh"; $O 'sudo bash -s enr' < $SP/peerloop_start_remote.sh; echo "[rc=$?]"
step copy the packaged owner tools to the guests "(root-owned, 0755, under /root/dns-owner-tools)"
$SCPG $OT/dns-peer-enroll celik@127.0.0.1:/tmp/dns-peer-enroll < /dev/null; echo "scp primary rc=$?"
$SCPO $OT/dns-peer-enroll $OT/bind-peer-inspect celik@127.0.0.1:/tmp/ < /dev/null; echo "scp secondary rc=$?"
run primary 'sudo install -d -o root -g root -m 0700 /root/dns-owner-tools && sudo install -o root -g root -m 0755 /tmp/dns-peer-enroll /root/dns-owner-tools/dns-peer-enroll && sudo sha256sum /root/dns-owner-tools/dns-peer-enroll'
run secondary 'sudo install -d -o root -g root -m 0700 /root/dns-owner-tools && sudo install -o root -g root -m 0755 /tmp/dns-peer-enroll /root/dns-owner-tools/dns-peer-enroll && sudo install -o root -g root -m 0755 /tmp/bind-peer-inspect /root/dns-owner-tools/bind-peer-inspect && sudo sha256sum /root/dns-owner-tools/*'
step status before
run primary 'sudo /root/dns-owner-tools/dns-peer-enroll primary-status'
run secondary 'sudo /root/dns-owner-tools/dns-peer-enroll secondary-status'
step primary-prepare "(primary owner)"
echo "\$ [primary] sudo /root/dns-owner-tools/dns-peer-enroll primary-prepare"
PREP=$($G 'sudo /root/dns-owner-tools/dns-peer-enroll primary-prepare' < /dev/null); echo "$PREP"; echo "[rc=$?]"
CRED=$(printf '%s' "$PREP" | python3 -c 'import json,sys;print(json.load(sys.stdin)["credential_id"])')
PUB=$(printf '%s' "$PREP" | python3 -c 'import json,sys;print(json.load(sys.stdin)["public_key"])')
echo "credential_id=$CRED"
test -n "$CRED" && test -n "$PUB" || { echo "PREPARE FAILED"; exit 1; }
step secondary owner saves the reviewed public key "(root-owned 0600 regular file /root/celikpanel-primary-inspector.pub)"
echo "\$ [secondary] (stdin: the displayed public key) sudo bash -c 'umask 077; test ! -e P; cat > P; chown root:root P; chmod 0600 P'"
printf '%s\n' "$PUB" | $O "sudo /bin/bash -c 'set -euo pipefail; umask 077; test ! -e /root/celikpanel-primary-inspector.pub; cat > /root/celikpanel-primary-inspector.pub; chown root:root /root/celikpanel-primary-inspector.pub; chmod 0600 /root/celikpanel-primary-inspector.pub; ls -la /root/celikpanel-primary-inspector.pub; sha256sum /root/celikpanel-primary-inspector.pub'"; echo "[rc=$?]"
step secondary-install "(secondary owner, BIND)"
run secondary "sudo /root/dns-owner-tools/dns-peer-enroll secondary-install --primary-ip $PIP --peer-ip $SIP --catalog $CAT --primary-public-key /root/celikpanel-primary-inspector.pub --inspector /root/dns-owner-tools/bind-peer-inspect"
step secondary-host-key
echo "\$ [secondary] sudo /root/dns-owner-tools/dns-peer-enroll secondary-host-key"
HK=$($O 'sudo /root/dns-owner-tools/dns-peer-enroll secondary-host-key' < /dev/null); echo "$HK"; echo "[rc=$?]"
DIG=$(printf '%s' "$HK" | python3 -c 'import json,sys;print(json.load(sys.stdin)["host_key_sha256"])')
step primary owner checks the digest independently "(SHA-256 of the decoded Ed25519 public key blob read from /etc/ssh/ssh_host_ed25519_key.pub; same SSH management channel, so this is a consistency check, not an out-of-band trust anchor)"
IND=$($O "awk '{print \$2}' /etc/ssh/ssh_host_ed25519_key.pub | base64 -d | sha256sum | cut -c1-64" < /dev/null | tr -d '\r\n')
echo "tool digest:        $DIG"; echo "independent digest: $IND"
[ "$DIG" = "$IND" ] && echo "DIGEST MATCH" || { echo "DIGEST MISMATCH: stop"; exit 1; }
step primary-activate "(primary owner)"
run primary "sudo /root/dns-owner-tools/dns-peer-enroll primary-activate --credential-id $CRED --primary-ip $PIP --peer-ip $SIP --catalog $CAT --host-key-sha256 $DIG"
step status after
run primary 'sudo /root/dns-owner-tools/dns-peer-enroll primary-status'
run secondary 'sudo /root/dns-owner-tools/dns-peer-enroll secondary-status'
step zone-lifecycle --zero-zones --recover-delete
COMMON="--work-root $ROOT --cell-id $CELL --node debian13 --identity-file $ROOT/id_ed25519 --source-fixture uninitialized"
echo "\$ python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete (dry run)"
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete < /dev/null; echo "[dry rc=$?]"
echo "\$ python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete --execute"
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py zone-lifecycle $COMMON --zero-zones --recover-delete --execute < /dev/null > $LOG/zone-lifecycle-recover.log 2>&1
rc=$?
echo "ZONE_LIFECYCLE_RECOVER_RC=$rc" | tee $LOG/zone-lifecycle-recover.rc
tail -n 5 $LOG/zone-lifecycle-recover.log | cut -c1-2000
step owner-enrollment done
