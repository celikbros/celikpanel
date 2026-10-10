#!/bin/bash
# set4. usage: bg.sh NAME script-file   -> runs the script detached, logs under /var/tmp/cp-set4-run/logs/NAME.*
set -u
name=$1; script=$2
R=/var/tmp/cp-set4-run; L=$R/logs
mkdir -p $L
[ -e $L/$name.start ] && { echo "refusing: $name already started"; exit 2; }
tr -d '\r' < "$script" > $R/job-$name.sh
date -u +%FT%TZ > $L/$name.start
echo "$(date -u +%FT%TZ) start $name" >> $R/progress.txt
setsid nohup bash -c "cd $R; bash $R/job-$name.sh > $L/$name.out 2> $L/$name.err; echo \$? > $L/$name.rc; date -u +%FT%TZ > $L/$name.end; echo \"\$(date -u +%FT%TZ) end $name rc=\$(cat $L/$name.rc)\" >> $R/progress.txt" < /dev/null > /dev/null 2>&1 &
disown
sleep 2
echo "started $name"
