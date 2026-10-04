#!/bin/bash
# Create run copy harness-h14 from harness-h12, patch H14, diff, hash, offline suite; write the Arch mgmt-off re-run job.
set -euo pipefail
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
[ -e $R/harness-h14 ] && { echo "refusing: harness-h14 exists"; exit 2; }
cp -a $R/harness-h12 $R/harness-h14
F=deploy/e2e/release-recovery/owner_update_trial.py
python3 $P/mk-h14.py $R/harness-h14/$F
cp -p $P/mk-h14.py $P/do-h14.sh $R/jobs/
( cd $R && diff -u harness-h12/$F harness-h14/$F > $R/H14-owner_update_trial.py.diff || true )
( cd $R && diff -u harness/$F harness-h14/$F > $R/H12-H13-H14-cumulative-owner_update_trial.py.diff || true )
wc -l $R/H14-owner_update_trial.py.diff $R/H12-H13-H14-cumulative-owner_update_trial.py.diff
cd $R/harness-h14 && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-h14-files.sha256
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py > $R/logs/offline-owner-h14.txt 2>&1; echo "owner suite rc=$?"
tail -n 3 $R/logs/offline-owner-h14.txt
ART=$(cat $R/ART)
cat > $R/jobs/job-cell-archmrb.sh <<EOF
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness-h14/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-mgmt-off-reboot $ART upd4-arch-mr-b 2531
EOF
echo "harness=$R/harness-h14 (git archive a6dd5b1e + H12/H13 + H14 run-copy diffs)" > $R/jobs/job-cell-archmrb.harness
cat $R/jobs/job-cell-archmrb.sh
