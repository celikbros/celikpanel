L=/var/tmp/cp-b10-1001/logs/z05-held-resume
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10/z05held.sh before-enroll
cut -c1-1200 $L/run-prepared.log | tail -n 20
cat $L/boot-monitor.log
cut -c1-700 $L/held-state-before-enroll.txt
head -n 12 $L/secondary-rndc-prerequisite-before-enroll.txt
