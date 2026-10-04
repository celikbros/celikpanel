SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
L=/var/tmp/cp-b11-1001/logs/z05-zero-started-zl-rb
cut -c1-2500 $L/resume.log
echo; cat $L/boot-monitor.log | cut -c1-200
bash $SP/post11.sh z05-zero-started-zl-rb pdns-switch__target-started__after-write__paired-primary__peer-reachable /var/tmp/cp-b11-1001/r2 "2026-09-30 05:31:44" "2026-09-30 05:32:05" after-resume
bash $SP/held11.sh z05-zero-started-zl-rb pdns-switch__target-started__after-write__paired-primary__peer-reachable /var/tmp/cp-b11-1001/r2 after-resume
