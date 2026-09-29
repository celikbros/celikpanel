# usage: extradiag.sh SHORT CELL ROOT [NODE] -> evidence/SHORT/extra-diagnostics-post-collect.txt (read-only)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch6b
python3 $SP/gssh.py $3 $2 ${4:-debian13} 'sudo bash -s' < $SP/extradiag_remote.sh > /var/tmp/cp-b6b-0930/evidence/$1/extra-diagnostics-post-collect.txt 2>&1
echo rc=$?
head -c 6000 /var/tmp/cp-b6b-0930/evidence/$1/extra-diagnostics-post-collect.txt
