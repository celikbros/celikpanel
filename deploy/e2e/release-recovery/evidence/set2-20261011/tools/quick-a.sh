#!/bin/bash
# make run copy NAME and run only the two new suites in the foreground (short)
set -u
name=$1
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2/mkset2.sh $name > /var/tmp/cp-set2-run/logs/mkov-$name.txt 2>&1; echo "mkov rc=$?"; tail -n 12 /var/tmp/cp-set2-run/logs/mkov-$name.txt
cd /var/tmp/cp-set2-run/harness-$name
export PYTHONDONTWRITEBYTECODE=1
for t in test_settings_writes_trial test_request_identity_trial; do python3 -m unittest deploy/e2e/release-recovery/$t.py 2>&1 | tail -n 25; done
