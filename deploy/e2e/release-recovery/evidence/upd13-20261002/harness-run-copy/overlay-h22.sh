#!/bin/bash
# upd13 H22: a second run copy (git archive f6cdd5a0) with exactly the two harness files from the working tree
# (CRLF normalized to LF), each listed with its SHA-256, and the diff against f6cdd5a0.
set -euo pipefail
R=/var/tmp/cp-upd13-run
C=f6cdd5a0
H=$R/harness-h22
O=$R/overlay-h22
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
[ -e $H ] && { echo "refusing: $H exists"; exit 2; }
mkdir -p $H $O
git -c safe.directory='*' -C "$REPO" archive $C | tar -x -C $H
: > $O/files.sha256; : > $O/harness.diff
for f in deploy/e2e/release-recovery/owner_update_trial.py deploy/e2e/release-recovery/test_owner_update_trial.py; do
  mode=0644; [ -x "$H/$f" ] && mode=0755
  sed 's/\r$//' "$REPO/$f" > "$H/$f.tmp"; install -m $mode "$H/$f.tmp" "$H/$f"; rm -f "$H/$f.tmp"
  sha256sum "$H/$f" | sed "s#$H/##" >> $O/files.sha256
  git -c safe.directory='*' -C "$REPO" show "$C:$f" | diff -u --label "a/$f" --label "b/$f" - "$H/$f" >> $O/harness.diff || true
done
sha256sum $O/harness.diff; cat $O/files.sha256; grep -c '^[-+][^-+]' $O/harness.diff
cd $H && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest deploy/e2e/release-recovery/test_owner_update_trial.py 2>&1 | tail -n 4
