#!/bin/bash
set -euo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/roottests
W=/var/tmp/cp-roottests-20261003
export PYTHONDONTWRITEBYTECODE=1
cd "$W"
cp -- "$SP/rtlab.py" "$W/rtlab.py"
printf 'install -d -m 0700 /root/celikpanel-release-recovery-lab\n' > "$W/mkdir.sh"
python3 rtlab.py run "$W/mkdir.sh" 60
python3 rtlab.py put "$W/go1.26.5.tar.gz" /root/celikpanel-release-recovery-lab/go1.26.5.tar.gz
python3 rtlab.py put "$W/src-48d264d5.tar" /root/celikpanel-release-recovery-lab/src-48d264d5.tar
cp -- "$SP/guest-provision.sh" "$W/guest-provision.sh"
python3 rtlab.py run "$W/guest-provision.sh" 3000
echo "== restrict network"
python3 rtlab.py restrict
cp -- "$SP/guest-netcheck.sh" "$W/guest-netcheck.sh"
python3 rtlab.py run "$W/guest-netcheck.sh" 120
echo HOST-PROVISION-DONE
