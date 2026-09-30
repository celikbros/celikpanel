#!/bin/bash
# usage: bg.sh NAME script-file   -> runs script detached, logs under /var/tmp/cp-upd2-run/logs/NAME.*
set -u
name=$1; script=$2
L=/var/tmp/cp-upd2-run/logs
mkdir -p $L
[ -e $L/$name.start ] && { echo "refusing: $name already started"; exit 2; }
date -u +%FT%TZ > $L/$name.start
setsid nohup bash -c "cd '/mnt/c/CELIKBROS PROJECTS/celikpanel'; bash '$script' > $L/$name.out 2> $L/$name.err; echo \$? > $L/$name.rc; date -u +%FT%TZ > $L/$name.end" < /dev/null > /dev/null 2>&1 &
disown
sleep 2
echo "started $name pid $!"
