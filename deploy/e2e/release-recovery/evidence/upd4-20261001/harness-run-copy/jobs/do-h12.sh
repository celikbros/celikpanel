#!/bin/bash
# Create run copy harness-h12 from the a6dd5b1e run copy, patch, diff, hash and run the offline suites on it.
set -euo pipefail
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
[ -e $R/harness-h12 ] && { echo "refusing: harness-h12 exists"; exit 2; }
cp -a $R/harness $R/harness-h12
F=deploy/e2e/release-recovery/owner_update_trial.py
python3 $P/mk-h12.py $R/harness-h12/$F
cp -p $P/mk-h12.py $P/do-h12.sh $R/jobs/
( cd $R && diff -u harness/$F harness-h12/$F > $R/H12-H13-owner_update_trial.py.diff || true )
wc -l $R/H12-H13-owner_update_trial.py.diff
cd $R/harness-h12 && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-h12-files.sha256
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py > $R/logs/offline-owner-h12.txt 2>&1; echo "owner suite rc=$?"
tail -n 3 $R/logs/offline-owner-h12.txt
python3 -m py_compile $R/harness-h12/$F && echo compiled
find $R/harness-h12 -name __pycache__ -type d -prune -exec rm -rf {} + 2>/dev/null; true
