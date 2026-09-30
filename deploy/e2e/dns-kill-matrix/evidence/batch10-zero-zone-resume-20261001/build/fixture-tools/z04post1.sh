SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b9-1001/r2; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b10-1001/logs/z04-resume
python3 $SP/gssh.py $ROOT $CELL arch 'sudo bash -s' < $SP/z04post_remote_sec.sh > $LOG/secondary-after-recover.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo cat /var/tmp/cp-b10-watch-led/timeline.log' < /dev/null > $LOG/ledwatch-timeline-snapshot.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -b -u celikpanel-agent.service --no-pager -o short-iso-precise | grep -v 'Panel certificate activation remains pending'" < /dev/null > $LOG/agent-journal-this-boot.txt 2>&1
cat $LOG/agent-journal-this-boot.txt | cut -c1-1500
cat $LOG/ledwatch-timeline-snapshot.txt | cut -c1-1200
cat $LOG/secondary-after-recover.txt
sed -n '/zone-sync ledger/,/agent-private/p' $LOG/primary-state-after-recover.txt
