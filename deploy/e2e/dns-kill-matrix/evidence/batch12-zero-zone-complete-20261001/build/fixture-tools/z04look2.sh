SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
ROOT=/var/tmp/cp-b12-1001/r1; CELL=pdns-switch__committed__after-write__paired-primary__peer-reachable
L=/var/tmp/cp-b12-1001/logs/z04-zero-committed-zl
echo "== secondary celikpeer/inspector lines"; grep -E 'celikpeer|bind-peer-inspect|zonestatus|127.0.0.1#' $L/secondary-after-recover.txt | cut -c1-230 | head -20
echo "== pdns journal CATALOG-HASH lines (primary, all boots)"
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -u pdns.service --no-pager -o short-iso-precise | grep -E 'CATALOG-HASH' | cut -c1-200" < /dev/null
echo "== agent journal 08:41:50-08:43:10 (non-certificate)"
python3 $SP/gssh.py $ROOT $CELL debian13 "sudo journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise --since '2026-09-30 08:41:50' --until '2026-09-30 08:43:10' | grep -v 'certificate activation' | cut -c1-420" < /dev/null
