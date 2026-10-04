SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
pgrep -af 'enroll11|gssh' | cut -c1-200 || echo "no leftover enrollment/ssh helper"
bash $SP/resumegate.sh
bash $SP/bg.sh z05resume.sh
