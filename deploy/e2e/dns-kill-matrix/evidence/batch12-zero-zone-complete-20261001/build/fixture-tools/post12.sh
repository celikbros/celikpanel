# usage: post12.sh SHORT CELL ROOT SINCE UNTIL TAG -- read-only: secondary journal window + inspector facts, ledger sampler snapshot, agent journal (all boots)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
SHORT=$1 CELL=$2 ROOT=$3 SINCE=$4 UNTIL=$5 TAG=$6
LOG=/var/tmp/cp-b12-1001/logs/$SHORT
python3 $SP/gssh.py $ROOT $CELL arch "sudo bash -s '$SINCE' '$UNTIL'" < $SP/post_remote_sec.sh > $LOG/secondary-$TAG.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo sh -c 'tail -n +1 /var/tmp/cp-b12-watch-led*/timeline.log'" < /dev/null > $LOG/ledwatch-timeline-snapshot.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise | grep -v 'Panel certificate activation remains pending'" < /dev/null > $LOG/agent-journal-all-boots.txt 2>&1
echo "== agent zone/peer/reason lines"
grep -iE 'zone|peer|pending|reason|inspect|dns_' $LOG/agent-journal-all-boots.txt | grep -v 'Started\|Starting' | cut -c1-600 | tail -n 40
echo "== secondary inspector lines"
grep -iE 'celikpeer|bind-peer-inspect|Accepted publickey|Connection closed by 192.0.2.10|reason' $LOG/secondary-$TAG.txt | grep -v audit | cut -c1-300 | tail -n 30
