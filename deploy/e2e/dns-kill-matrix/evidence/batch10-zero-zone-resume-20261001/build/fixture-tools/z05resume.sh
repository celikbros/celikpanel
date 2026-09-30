SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
bash $SP/z05held.sh before-resume
bash $SP/runresume.sh z05-held-resume pdns-switch__target-started__after-write__paired-primary__peer-reachable /var/tmp/cp-b10-1001/r1 --zero-zones --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot
L=/var/tmp/cp-b10-1001/logs/z05-held-resume
cat $L/resume.rc
cut -c1-2500 $L/resume.log
tail -n 8 $L/driver.log
cat $L/boot-monitor.log
