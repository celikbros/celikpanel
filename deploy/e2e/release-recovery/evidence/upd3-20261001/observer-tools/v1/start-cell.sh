#!/bin/bash
# usage: start-cell.sh SHORT CELL LAB PORT NODE   (ART from /var/tmp/cp-upd3-run/ART)
set -u
short=$1 cell=$2 labname=$3 port=$4 node=$5
R=/var/tmp/cp-upd3-run
ART=$(cat $R/ART)
pgrep -a qemu && { echo "QEMU already running; refusing"; exit 2; }
[ -e /var/tmp/cp-release-drill-$labname ] && { echo "lab exists; refusing"; exit 2; }
free -g | head -2
mkdir -p $R/jobs
cat > $R/jobs/job-cell-$short.sh <<J
#!/bin/bash
exec bash /var/tmp/cp-upd3-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell $cell $ART $labname $port
J
cat > $R/jobs/job-side-$short.sh <<J
#!/bin/bash
exec bash /var/tmp/cp-upd3-run/tools/sidecar.sh cell-$short $labname $node
J
bash $R/tools/bg.sh cell-$short $R/jobs/job-cell-$short.sh
bash $R/tools/bg.sh side-$short $R/jobs/job-side-$short.sh
