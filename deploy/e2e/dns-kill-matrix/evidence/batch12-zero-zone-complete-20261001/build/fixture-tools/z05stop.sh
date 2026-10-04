SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
cut -c1-900 /var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb/extra-observe-after-pending.txt
grep -E 'rc=|request id' /var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb/collect.log
bash $SP/stop12.sh z05-zero-started-zl-rb pdns-switch__target-started__after-write__paired-primary__peer-reachable /var/tmp/cp-b12-1001/r2
bash $SP/derive12.sh
