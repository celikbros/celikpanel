SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
# usage: rndcfacts.sh ROOT CELL OUTFILE
python3 $SP/gssh.py $1 $2 arch 'sudo bash -s' < $SP/rndcfacts_remote.sh > $3 2>&1
cat $3
