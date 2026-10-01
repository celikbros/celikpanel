#!/bin/bash
# Rest of do-h14.sh after the offline suite (it stopped on the one test that pins the old cut, recorded).
set -euo pipefail
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
cp -p $P/mk-h14.py $P/do-h14.sh $P/job-h14.sh $R/jobs/
find $R/harness-h14 -name __pycache__ -type d -print
ART=$(cat $R/ART)
cat > $R/jobs/job-cell-archmrb.sh <<EOF
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness-h14/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-mgmt-off-reboot $ART upd4-arch-mr-b 2531
EOF
echo "harness=$R/harness-h14 (git archive a6dd5b1e + H12/H13 + H14 run-copy diffs)" > $R/jobs/job-cell-archmrb.harness
cat $R/jobs/job-cell-archmrb.sh
