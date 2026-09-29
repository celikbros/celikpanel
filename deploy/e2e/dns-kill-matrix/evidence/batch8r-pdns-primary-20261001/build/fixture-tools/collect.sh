#!/bin/bash
# usage: collect.sh SHORT CELL ROOT -- read-only capture from both guests into LROOT/evidence/SHORT
set -uo pipefail
SHORT=$1 CELL=$2 ROOT=$3
NODE=debian13; PNODE=arch
LROOT=/var/tmp/cp-b8r-1001
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r
LOG=$LROOT/logs/$SHORT
E=$LROOT/evidence/$SHORT
mkdir -p $E
exec >> $LOG/collect.log 2>&1
G="python3 $SP/gssh.py $ROOT $CELL $NODE"
P="python3 $SP/gssh.py $ROOT $CELL $PNODE"
echo "### collect $(date -u +%FT%T.%NZ)"
W=$(grep -o '/var/tmp/cp-b8r-watch[a-z0-9-]*' $LOG/boot-monitor.log | tail -n1)
echo "last watcher dir: $W"
for i in $(seq 1 60); do $G "sudo grep -q '^done' $W/timeline.log" < /dev/null && break; sleep 1; done
$G 'sudo bash -s' < $SP/unitfacts_remote.sh > $LOG/post-state.txt 2>&1
$G 'sudo bash -s' < $SP/versions_remote.sh > $E/versions-post-collect-guest.txt 2>&1
$P 'sudo bash -s' < $SP/versions_remote.sh > $E/versions-post-collect-peer.txt 2>&1
$(python3 $SP/scpcmd.py $ROOT $CELL $NODE) /root/cp-b8r-tools/oi-smstatus celik@127.0.0.1:/tmp/oi-smstatus < /dev/null
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
$G 'sudo python3 - /var/lib/powerdns/pdns.sqlite3' < $SP/pdnsdb.py > $E/pdns-db-post-collect.txt 2>&1
echo "pdnsdb rc=$?"
$G 'sudo bash -s' < $SP/primstate_remote.sh > $E/primary-state-post-collect.txt 2>&1
$G 'sudo python3 -' < $SP/pairq.py > $E/dns-post-collect-guest.txt 2>&1
$P 'sudo python3 -' < $SP/pairq.py > $E/dns-post-collect-peer.txt 2>&1
$P 'sudo bash -s' < $SP/bindsec_remote.sh > $E/bind-secondary-state-post-collect.txt 2>&1
echo "bindsec rc=$?"
$P 'sudo bash -s bind' < $SP/peerfacts_remote.sh > $E/peer-facts-post-collect.txt 2>&1
$P 'sudo bash -s bind' < $SP/peerjournal_remote.sh > $E/peer-daemon-journal.txt 2>&1
echo "peer journal rc=$?"
$P 'for u in $(systemctl list-units --all --plain --no-legend "cp-b8r-peerloop*" | cut -d" " -f1); do sudo systemctl stop $u; done; for d in /var/tmp/cp-b8r-peerloop*; do echo "######## $d"; sudo cat $d/samples.log; done' > $E/peer-dns-sampler.log 2> $LOG/peerloop.stderr < /dev/null
echo "peerloop rc=$? lines=$(wc -l < $E/peer-dns-sampler.log)"
$G "sudo journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise | grep -iE 'catalog format|catalog producer|re-stamp|restamp'" > $E/catalog-lines-agent-journal.txt 2>&1 < /dev/null
CD=$(python3 -c 'import sys;sys.path.insert(0,"/root/cp-b8r-src/deploy/e2e/dns-kill-matrix");import fixture;from pathlib import Path;print(fixture.load_cell_plan(Path(sys.argv[1]).resolve(),sys.argv[2])["cell_directory"])' $ROOT $CELL)
echo "cell directory: $CD"
if [ -d $CD/fresh-primary-peer ]; then mkdir -p $E/fresh-primary-peer && cp -a $CD/fresh-primary-peer/. $E/fresh-primary-peer/; ls -la $E/fresh-primary-peer; else echo "no fresh-primary-peer directory"; fi
$G 'sudo touch /var/tmp/cp-b8r-dbwatch.stop; sleep 2; sudo tail -n 3 /var/tmp/cp-b8r-watch-db*/timeline.log' < /dev/null
$G 'sudo bash -s' < $SP/guesttar_remote.sh > $LOG/guest-evidence.tar 2> $LOG/guest-tar.stderr
echo "tar rc=$? size=$(stat -c %s $LOG/guest-evidence.tar)"
mkdir -p $E/raw && tar -C $E/raw -xf $LOG/guest-evidence.tar
cp $LOG/*.log $LOG/*.json $LOG/*.txt $LOG/run-prepared.rc $E/ 2>/dev/null
cp $LOG/smstatus.stderr $E/ 2>/dev/null
if [ -f $E/raw/results/$CELL/result.json ]; then python3 $SP/extract.py $E/raw/results/$CELL/result.json > $E/native-and-outage.json 2> $LOG/extract.stderr; echo "extract rc=$?"; fi
if [ -n "$(find $E/raw/watch -name '*.sqlite3' 2>/dev/null | head -1)" ]; then
  : > $E/pdns-db-timeline.txt
  for f in $(find $E/raw/watch -name '*.sqlite3' | sort); do echo "######## $f" >> $E/pdns-db-timeline.txt; python3 $SP/pdnsdb.py $f copy >> $E/pdns-db-timeline.txt 2>&1; done
  python3 $SP/serials.py $E > $E/catalog-serial-history.txt 2>&1
fi
$G "sudo journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise | grep -iE 'fail-closed|ambiguous ledger|hold|DNS-only|released|recovery is unknown|rolled back|rollback|owner'" > $E/agent-journal-hold-rollback-lines.txt 2>&1 < /dev/null
find $E -type f | sort
echo "### collect done $(date -u +%FT%T.%NZ)"
