D=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8
bash $D/summ.sh $1 $2 2>&1 | grep -v '^/var/tmp' | cut -c1-1200
bash $D/bc.sh $1 $2 2>&1
