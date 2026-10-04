#!/bin/bash
# Usage: g.sh <guest-body-script-basename> [timeout]   -> runs it guarded as root in the guest.
#        g.sh put <scratch-file-basename>               -> uploads to the guest's private lab dir.
set -euo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/roottests
W=/var/tmp/cp-roottests-20261003
export PYTHONDONTWRITEBYTECODE=1
cd "$W"
if [ "$1" = put ]; then
    cp -- "$SP/$2" "$W/$2"
    python3 rtlab.py put "$W/$2" "/root/celikpanel-release-recovery-lab/$2"
    exit 0
fi
cp -- "$SP/$1" "$W/$1"
python3 rtlab.py run "$W/$1" "${2:-120}"
