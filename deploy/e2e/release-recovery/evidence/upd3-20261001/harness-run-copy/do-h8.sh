#!/bin/bash
R=/var/tmp/cp-upd3-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
PYTHONDONTWRITEBYTECODE=1 python3 -P $P/mk-h8.py || exit 1
cd $R/harness-h8
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py > $R/logs/offline-owner-h8.txt 2>&1; echo "owner-h8 rc=$?"
tail -n 3 $R/logs/offline-owner-h8.txt
cd $R/harness-h8 && find . -type f -print0 | sort -z | xargs -0 sha256sum > $R/runcopy-h8-files.sha256
diff <(cut -c67- $R/runcopy-files.sha256) <(cut -c67- $R/runcopy-h8-files.sha256) | head; diff $R/runcopy-files.sha256 $R/runcopy-h8-files.sha256 | head -4
