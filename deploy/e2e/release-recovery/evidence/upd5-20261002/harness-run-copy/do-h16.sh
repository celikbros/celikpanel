#!/bin/bash
# H16 run copy: harness-h16 = the 6cda60b8 run copy + mk-h16.py; diff, file hashes and offline suites recorded.
set -euo pipefail
R=/var/tmp/cp-upd5-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd5run
[ -e $R/harness-h16 ] && { echo "refusing: harness-h16 exists"; exit 2; }
cp -a $R/harness $R/harness-h16
python3 $P/mk-h16.py $R/harness-h16
cd $R
diff -u harness/deploy/e2e/release-recovery/owner_update_trial.py harness-h16/deploy/e2e/release-recovery/owner_update_trial.py > $R/H16-owner_update_trial.py.diff || true
wc -l $R/H16-owner_update_trial.py.diff
cd $R/harness-h16 && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-h16-files.sha256
diff <(cut -c67- $R/runcopy-files.sha256) <(cut -c67- $R/runcopy-h16-files.sha256) > /dev/null && echo "same file list"
diff $R/runcopy-files.sha256 $R/runcopy-h16-files.sha256 | grep '^[<>]' || true
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py > $R/logs/offline-owner-h16.txt 2>&1 && echo "offline owner h16 ok" || echo "offline owner h16 FAILED"
tail -n 3 $R/logs/offline-owner-h16.txt
python3 -m py_compile deploy/e2e/release-recovery/owner_update_trial.py 2>&1 || true
find $R/harness-h16 -name __pycache__ -newer $R/H16-owner_update_trial.py.diff -print
cat > $R/jobs/job-cell-archocb.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness-h16/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-owner-continuation $(cat $R/ART) upd5-arch-oc-b 2601
EOT
echo "harness=$R/harness-h16 (git archive 6cda60b8 + H16)" > $R/jobs/job-cell-archocb.harness
cat $R/jobs/job-cell-archocb.sh
