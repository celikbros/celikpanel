#!/bin/bash
set -u
R=/var/tmp/cp-release-drill-upd2-diag-arch
H="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery"
S=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd2run
pgrep -a qemu && { echo "QEMU running; refusing"; exit 2; }
[ -e $R ] && { echo "exists"; exit 2; }
python3 "$H/lab.py" prepare --work-root $R --image-cache /var/tmp/cp-v3n28/images --ssh-port 2401 --execute
python3 "$H/lab.py" start --work-root $R --execute
PYTHONDONTWRITEBYTECODE=1 python3 -P $S/tools/diag.py $R arch > /var/tmp/cp-upd2-run/logs/diag-arch-resolver.txt 2>&1; echo "diag rc=$?"
python3 "$H/lab.py" stop --work-root $R --execute
cat /var/tmp/cp-upd2-run/logs/diag-arch-resolver.txt
