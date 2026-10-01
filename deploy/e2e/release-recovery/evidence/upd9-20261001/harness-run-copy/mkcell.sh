#!/bin/bash
# usage: mkcell.sh SHORT CELL ARTIFACTS LAB PORT  -> writes /var/tmp/cp-upd9-run/jobs/job-cell-SHORT.sh
set -eu
R=/var/tmp/cp-upd9-run
short=$1 cell=$2 art=$3 lab=$4 port=$5
cat > $R/jobs/job-cell-$short.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness/deploy/e2e/release-recovery/run-upd1.sh cell $cell $art $lab $port
EOT
echo "harness=$R/harness (git archive efcba145 + overlay $(sha256sum $R/overlay/harness.diff | cut -c1-16))" > $R/jobs/job-cell-$short.harness
cat $R/jobs/job-cell-$short.sh $R/jobs/job-cell-$short.harness
