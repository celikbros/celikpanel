# read-only look after the z04 resume
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
ROOT=/var/tmp/cp-b11-1001/r1; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
L=/var/tmp/cp-b11-1001/logs/z04-zero-committed-zl
sed -n '/all lines from/,/dns-peer-enroll secondary-status/p' $L/secondary-after-recover.txt | cut -c1-260
echo "== ledger sampler (7fb6a0df lines)"
grep -E '7fb6a0df' $L/ledwatch-timeline-snapshot.txt | grep -oE '^[0-9:.]+ CHANGE|7fb6a0df [a-z]+ [a-z-]+ att=[0-9]+ lease=[^ ]+ code=[a-z_:]*' | paste - - | uniq -f1 -c | tail -15
echo "== pdns journal catalog lines (primary)"
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -u pdns.service --no-pager -o short-iso-precise | grep -iE 'CATALOG-HASH|catalog|AXFR|notif' | cut -c1-260" < /dev/null
echo "== dbwatch timeline"
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo cat /var/tmp/cp-b11-watch-db/timeline.log | cut -c1-200" < /dev/null
