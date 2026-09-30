# usage: bg.sh SCRIPT ARGS... -- run a b11 script detached (setsid nohup); output to /var/tmp/cp-b12-1001/logs/bg-<script>.out
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
S=$1; shift
setsid -f nohup bash $SP/$S "$@" > /var/tmp/cp-b12-1001/logs/bg-${S%.sh}.out 2>&1 < /dev/null
echo started $S
