#!/bin/bash
# end of the harness debugging session: stop the lab again (disks and evidence retained until staging)
export PYTHONDONTWRITEBYTECODE=1
cd /var/tmp/cp-set2-run/harness-dev
python3 deploy/e2e/release-recovery/lab.py stop --work-root /var/tmp/cp-release-drill-rid-ub-a --execute
pgrep -af "cp-release-drill-rid-ub-a/" | cut -c1-100 || echo "no process of the lab left"
ls /var/tmp/cp-release-drill-rid-ub-a/evidence/ubuntu/upd1/
