# usage: g.sh ROOT CELL NODE SCRIPT [ARGS] -- run a local script on a guest via sudo bash -s
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8
ROOT=$1 CELL=$2 NODE=$3 S=$4; shift 4
python3 $SP/gssh.py $ROOT $CELL $NODE "sudo bash -s $*" < $S
