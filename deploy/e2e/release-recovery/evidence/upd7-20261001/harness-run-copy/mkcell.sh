#!/bin/bash
# usage: mkcell.sh SHORT CELL LAB PORT  -> writes /var/tmp/cp-upd7-run/jobs/job-cell-SHORT.sh
set -eu
R=/var/tmp/cp-upd7-run
short=$1 cell=$2 lab=$3 port=$4
ART=$(cat $R/ART)
cat > $R/jobs/job-cell-$short.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness/deploy/e2e/release-recovery/run-upd1.sh cell $cell $ART $lab $port
EOT
echo "harness=$R/harness (git archive 48d21d58 + overlay $(sha256sum $R/overlay/harness.diff | cut -c1-16))" > $R/jobs/job-cell-$short.harness
cat $R/jobs/job-cell-$short.sh
