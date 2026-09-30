SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
CELL=pdns-switch__target-started__after-write__paired-primary__peer-reachable; ROOT=/var/tmp/cp-b12-1001/r2; S=z05-zero-started-zl-rb
bash $SP/held12.sh $S $CELL $ROOT before-resume
bash $SP/runresume.sh $S $CELL $ROOT --zero-zones --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot
L=/var/tmp/cp-b12-1001/logs/$S
cat $L/resume.rc
echo Z05-RESUME-DONE
