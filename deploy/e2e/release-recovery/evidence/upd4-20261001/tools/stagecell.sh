#!/bin/bash
# usage: stagecell.sh SHORT LAB NODE CELL [RUN]  -> copies one finished cell into the stage (host only, read-only on the lab)
set -u
short=$1; lab=$2; node=$3; cell=$4; run=${5:-run-a}
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
S=$R/stage/upd4-20261001
L=/var/tmp/cp-release-drill-$lab
D=$S/$cell/$run
[ -e $D ] && { echo "refusing: $D exists"; exit 2; }
mkdir -p $D/host $D/side
ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
cp -a $ev/. $D/
( cd $D && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 && echo "$cell driver SHA256SUMS ok" || echo "$cell driver SHA256SUMS FAILED" )
for x in start end rc out err; do cp -p $R/logs/cell-$short.$x $D/host/wrapper.$x.txt 2>/dev/null; done
cp -p $R/jobs/job-cell-$short.sh $D/host/job.sh
cp -p $R/jobs/job-cell-$short.harness $D/host/harness.txt
echo "lab=$L" > $D/host/lab.txt; basename $ev >> $D/host/lab.txt
for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp -p "$f" $D/host/; done
cp -p $L/lab.json $L/current-worker-baseline-$node-current-worker-baseline-result.json $L/current-worker-baseline-$node-current-worker-baseline.log $D/host/ 2>/dev/null
cp -p $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest $D/host/ 2>/dev/null
cp -p $L/cells/*/fixture-plan.json $D/host/ 2>/dev/null
python3 $P/ext4.py $lab $node $D/side/extract.txt
find $D -type f | wc -l; du -sh $D
