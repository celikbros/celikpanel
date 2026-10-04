#!/bin/bash
# usage: collect.sh SHORT CELL  -- read-only capture from guest into ROOT/evidence/SHORT
set -uo pipefail
SHORT=$1 CELL=$2 NODE=debian13
ROOT=/var/tmp/cp-b4-0929
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch4
LOG=$ROOT/logs/$SHORT
E=$ROOT/evidence/$SHORT
mkdir -p $E
exec >> $LOG/collect.log 2>&1
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
echo "### collect $(date -u +%FT%T.%NZ)"
W=$(grep -o '/var/tmp/cp-b4-watch[a-z0-9-]*' $LOG/boot-monitor.log | tail -n1)
echo "last watcher dir: $W"
for i in $(seq 1 60); do $G "sudo grep -q '^done' $W/timeline.log" < /dev/null && break; sleep 1; done
$G 'sudo bash -s' < $SP/unitfacts_remote.sh > $LOG/post-state.txt 2>&1
scp_cmd=$(python3 - <<PY
import sys; sys.path.insert(0,"/root/cp-b4-src/deploy/e2e/dns-kill-matrix")
import fixture, guest_bootstrap, shlex
from pathlib import Path
plan=fixture.load_cell_plan(Path("$ROOT"),"$CELL")
print(shlex.join(guest_bootstrap.scp_base(plan["nodes"]["$NODE"],Path("$ROOT/id_ed25519"))))
PY
)
$scp_cmd /root/cp-b4-tools/oi-smstatus celik@127.0.0.1:/tmp/oi-smstatus < /dev/null
RID=$($G "sudo python3 -c 'import json;print(json.load(open(\"/var/lib/celikpanel-dns-kill-matrix/results/$CELL/result.json\"))[\"request_id\"])'" < /dev/null)
echo "request id: $RID"
$G "sudo bash -s $RID" < $SP/ownerpost_remote.sh > $E/owner-post-state.txt 2>&1
echo "ownerpost rc=$?"
$G "sudo install -m 0700 /tmp/oi-smstatus /root/oi-smstatus && sudo sha256sum /root/oi-smstatus && sudo /root/oi-smstatus $RID" > $E/service-mutation-status-post-collect.json 2> $LOG/smstatus.stderr < /dev/null
echo "smstatus rc=$?"
$G 'sudo /usr/bin/python3 -c "import json;a=json.load(open(\"/var/lib/celikpanel-dns-kill-matrix/controller-argv.json\"));i=a.index(\"--recovery-probe-command\");print(json.dumps(json.loads(a[i+1])))"' > $LOG/probe-argv.json 2>&1 < /dev/null
PROBE=$(python3 -c 'import json,shlex,sys;print(shlex.join(json.load(open(sys.argv[1]))))' $LOG/probe-argv.json)
$G "sudo /usr/sbin/runuser -u root -g celikpanel -- /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C.UTF-8 $PROBE" > $E/guest-recovery-probe-post-collect.json 2> $LOG/probe-post.stderr < /dev/null
echo "probe rc=$?"
$G 'sudo python3 -' < $SP/dnsq.py > $E/dns-post-collect.txt 2>&1
$G 'sudo python3 -' < $SP/soaq.py > $E/soa-post-collect.txt 2>&1
$G 'sudo bash -s' < $SP/guesttar_remote.sh > $LOG/guest-evidence.tar 2> $LOG/guest-tar.stderr
echo "tar rc=$? size=$(stat -c %s $LOG/guest-evidence.tar)"
mkdir -p $E/raw && tar -C $E/raw -xf $LOG/guest-evidence.tar
cp $LOG/*.log $LOG/*.json $LOG/*.txt $LOG/run-prepared.rc $E/ 2>/dev/null
cp $LOG/smstatus.stderr $E/ 2>/dev/null
if [ -f $E/raw/results/$CELL/result.json ]; then python3 $SP/extract.py $E/raw/results/$CELL/result.json > $E/native-and-outage.json 2> $LOG/extract.stderr; echo "extract rc=$?"; fi
find $E -type f | sort
echo "### collect done $(date -u +%FT%T.%NZ)"
