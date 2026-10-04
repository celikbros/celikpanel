#!/bin/bash
# usage: mkcell.sh SHORT CELL ARTIFACTS LAB PORT  -> writes /var/tmp/cp-upd13-run/jobs/job-cell-SHORT.sh
# upd13: the run copy is $R/harness unless $R/current-harness.txt names another copy (H22: harness-h22, overlay-h22).
set -eu
R=/var/tmp/cp-upd13-run
short=$1 cell=$2 art=$3 lab=$4 port=$5
h=harness; [ -s $R/current-harness.txt ] && h=$(cat $R/current-harness.txt)
o=overlay${h#harness}
cat > $R/jobs/job-cell-$short.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/$h/deploy/e2e/release-recovery/run-upd1.sh cell $cell $art $lab $port
EOT
if [ -s $R/$o/harness.diff ]; then ov="overlay $o $(sha256sum $R/$o/harness.diff | cut -c1-16)"; else ov="no overlay"; fi
echo "harness=$R/$h (git archive f6cdd5a0, $ov)" > $R/jobs/job-cell-$short.harness
cat $R/jobs/job-cell-$short.sh $R/jobs/job-cell-$short.harness
