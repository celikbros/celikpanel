SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b10-1001/r1; CELL=pdns-switch__target-started__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b10-1001/logs/z05-held-resume
bash $SP/z05held.sh after-resume
python3 $SP/gssh.py $ROOT $CELL arch 'sudo bash -s' < $SP/z05post_remote_sec.sh > $LOG/secondary-after-resume.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -b -u celikpanel-agent.service --no-pager -o short-iso-precise | grep -v 'Panel certificate activation remains pending'" < /dev/null > $LOG/agent-journal-this-boot.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo cat /var/tmp/cp-b10-watch-led/timeline.log' < /dev/null > $LOG/ledwatch-timeline-snapshot.txt 2>&1
grep -E 'agent\[' $LOG/agent-journal-this-boot.txt | grep -iE 'zone|peer|pending|dns_' | cut -c1-500
awk '{print $1, $4, $5, $6}' $LOG/ledwatch-timeline-snapshot.txt | head -3
grep -oE '^[0-9:.]+ CHANGE ledger|[0-9a-f]{8} [a-z]+ [a-z-]+ att=[0-9]+ lease=[^ ]+ code=[a-z_]*' $LOG/ledwatch-timeline-snapshot.txt | grep -E 'CHANGE|^1925' | paste - - | awk '{print $1, $4, $5, $6, $7, $8, $9}' | uniq -f1 -c | head -40
grep -iE 'celikpeer|bind-peer-inspect|Accepted publickey for celikpeer|Connection closed by 192.0.2.10' $LOG/secondary-after-resume.txt | grep -v audit | cut -c1-250
grep -E 'AXFR|NOTAUTH|notify' $LOG/secondary-after-resume.txt | cut -c1-250 | head
grep -E 'result.json present|reboot-checkpoint-1.json$|^[0-9a-f]{64}  /var/lib' $LOG/held-state-after-resume.txt
grep -E 'boot_id=' $LOG/held-state-after-resume.txt
ls -la --time-style=full-iso /var/tmp/cp-b10-1001/r1/cells/e05b4787635364e83414f682/fresh-primary-peer
