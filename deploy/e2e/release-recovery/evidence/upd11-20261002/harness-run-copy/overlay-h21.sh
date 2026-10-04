#!/bin/bash
# upd11: reset the run copy to git archive 48e54657, then apply exactly the listed harness
# files from the working tree (CRLF normalized to LF, as git does), each listed with its SHA-256.
set -euo pipefail
R=/var/tmp/cp-upd11-run
C=48e54657
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
mkdir -p $R/harness-h21 && git -c safe.directory="*" -C "$REPO" archive $C | tar -x -C $R/harness-h21
files="deploy/e2e/release-recovery/owner_update_trial.py
deploy/e2e/release-recovery/test_owner_update_trial.py
${EXTRA_OVERLAY:-}"
mkdir -p $R/overlay-h21
: > $R/overlay-h21/files.sha256
: > $R/overlay-h21/harness.diff
for f in $files; do
  [ -n "$f" ] || continue
  case $f in deploy/e2e/*) ;; *) echo "refusing $f"; exit 2;; esac
  mode=0644; [ -x "$R/harness-h21/$f" ] && mode=0755
  sed 's/\r$//' "$REPO/$f" > "$R/harness-h21/$f.upd11h21tmp"
  install -m $mode "$R/harness-h21/$f.upd11h21tmp" "$R/harness-h21/$f"
  rm -f "$R/harness-h21/$f.upd11h21tmp"
  sha256sum "$R/harness-h21/$f" | sed "s#$R/harness-h21/##" >> $R/overlay-h21/files.sha256
  { git -c safe.directory='*' -C "$REPO" show "$C:$f" 2>/dev/null || true; } | diff -u --label "a/$f" --label "b/$f" - "$R/harness-h21/$f" >> $R/overlay-h21/harness.diff || true
done
sha256sum $R/overlay-h21/harness.diff
cat $R/overlay-h21/files.sha256
grep -c '^[-+][^-+]' $R/overlay-h21/harness.diff || true
