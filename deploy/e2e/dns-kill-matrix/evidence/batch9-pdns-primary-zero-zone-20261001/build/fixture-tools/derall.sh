SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
tail -3 /var/tmp/cp-b9-1001/logs/chain.log
pgrep -a qemu | cut -c1-100 || echo "no qemu"
bash $SP/derive.sh
bash $SP/derive2.sh
