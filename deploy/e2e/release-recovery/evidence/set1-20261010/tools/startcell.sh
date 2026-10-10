#!/bin/bash
# usage: startcell.sh SHORT CELL LAB PORT [HARNESS]  -> writes the job and starts it detached (one cell at a time).
set -eu
R=/var/tmp/cp-set1-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set1
short=$1 cell=$2 lab=$3 port=$4 h=${5:-harness-set1}
if pgrep -f qemu-system > /dev/null; then echo "refusing: a QEMU is running"; exit 2; fi
[ -e /var/tmp/cp-release-drill-$lab ] && { echo "refusing: lab $lab exists"; exit 2; }
art=$(cat $R/art-cur.txt)
o=overlay${h#harness}
cat > $R/jobs/job-cell-$short.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/$h/deploy/e2e/release-recovery/run-set1.sh cell $cell $art $lab $port
EOT
echo "harness=$R/$h (git archive c4cf7fd9, overlay $o $(sha256sum $R/$o/harness.diff | cut -c1-16))" > $R/jobs/job-cell-$short.harness
cat $R/jobs/job-cell-$short.sh $R/jobs/job-cell-$short.harness
bash $P/bg.sh cell-$short $R/jobs/job-cell-$short.sh
