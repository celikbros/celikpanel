# usage: extradiag.sh SHORT CELL  -> evidence/SHORT/extra-diagnostics-post-collect.txt (read-only)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch4
ROOT=/var/tmp/cp-b4-0929
python3 $SP/gssh.py $ROOT $2 debian13 'sudo bash -s' < $SP/extradiag_remote.sh > $ROOT/evidence/$1/extra-diagnostics-post-collect.txt 2>&1
echo rc=$?
cat $ROOT/evidence/$1/extra-diagnostics-post-collect.txt
