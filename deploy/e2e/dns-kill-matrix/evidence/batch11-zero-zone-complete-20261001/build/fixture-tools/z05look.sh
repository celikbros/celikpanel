SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
ROOT=/var/tmp/cp-b11-1001/r2; CELL=pdns-switch__target-started__after-write__paired-primary__peer-reachable
L=/var/tmp/cp-b11-1001/logs/z05-zero-started-zl-rb
echo "== secondary window (named/sshd/sudo lines)"
sed -n '/all lines from/,/dns-peer-enroll secondary-status/p' $L/secondary-after-resume.txt | grep -E 'named|celikpeer|inspect' | cut -c1-260
echo "== ledger sampler"
grep -E '19255434' $L/ledwatch-timeline-snapshot.txt | grep -oE '^[0-9:.]+ CHANGE|19255434 [a-z]+ [a-z-]+ att=[0-9]+ lease=[^ ]+ code=[a-z_:]*' | paste - - | awk '{print $1,$4,$5,$6,$7,$8,$9}' | sed -n '1,3p;$p'
grep -cE '19255434 running' $L/ledwatch-timeline-snapshot.txt
echo "== primary pdns journal 05:31:40-05:32:10"
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -u pdns.service --no-pager -o short-iso-precise --since '2026-09-30 05:31:40' --until '2026-09-30 05:32:10' | cut -c1-240" < /dev/null
echo "== primary agent journal 05:31:40-05:32:10"
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise --since '2026-09-30 05:31:40' --until '2026-09-30 05:32:10' | grep -v 'certificate activation' | cut -c1-400" < /dev/null
