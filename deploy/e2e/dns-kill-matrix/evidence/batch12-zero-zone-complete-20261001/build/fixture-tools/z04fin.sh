SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
C=pdns-switch__committed__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r1; S=z04-zero-committed-zl
bash $SP/collect12.sh $S $C $R > /dev/null 2>&1
grep -E 'rc=|request id|cell directory' /var/tmp/cp-b12-1001/logs/$S/collect.log
bash $SP/stop12.sh $S $C $R
echo Z04-FIN-DONE
