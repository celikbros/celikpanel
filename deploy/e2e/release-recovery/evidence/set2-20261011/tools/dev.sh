#!/bin/bash
# set2: a mutable development copy (never used for a cell): refresh the listed files and run the two new suites.
set -u
R=/var/tmp/cp-set2-run
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
if [ ! -d $R/harness-dev ]; then
  mkdir -p $R/harness-dev && git -c safe.directory='*' -C "$REPO" archive faa5ef085 | tar -x -C $R/harness-dev
fi
for b in $(cat /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2/files.txt); do
  f=deploy/e2e/release-recovery/$b
  sed 's/\r$//' "$REPO/$f" > $R/harness-dev/$f
done
chmod 755 $R/harness-dev/deploy/e2e/release-recovery/run-set2.sh
cd $R/harness-dev
export PYTHONDONTWRITEBYTECODE=1
for t in ${1:-test_settings_writes_trial test_request_identity_trial}; do python3 -m unittest deploy/e2e/release-recovery/$t.py 2>&1 | tail -n ${2:-30}; done
bash -n deploy/e2e/release-recovery/run-set2.sh && echo "run-set2.sh syntax ok"
