#!/bin/bash
# upd12: reset the run copy to git archive 6b6f8a0c, then apply exactly the listed harness
# files from the working tree (CRLF normalized to LF, as git does), each listed with its SHA-256.
set -euo pipefail
R=/var/tmp/cp-upd12-run
C=6b6f8a0c
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
git -c safe.directory='*' -C "$REPO" archive $C | tar -x -C $R/harness
files="deploy/e2e/release-recovery/owner_update_trial.py
deploy/e2e/release-recovery/test_owner_update_trial.py
${EXTRA_OVERLAY:-}"
mkdir -p $R/overlay
: > $R/overlay/files.sha256
: > $R/overlay/harness.diff
for f in $files; do
  [ -n "$f" ] || continue
  case $f in deploy/e2e/*) ;; *) echo "refusing $f"; exit 2;; esac
  mode=0644; [ -x "$R/harness/$f" ] && mode=0755
  sed 's/\r$//' "$REPO/$f" > "$R/harness/$f.upd12tmp"
  install -m $mode "$R/harness/$f.upd12tmp" "$R/harness/$f"
  rm -f "$R/harness/$f.upd12tmp"
  sha256sum "$R/harness/$f" | sed "s#$R/harness/##" >> $R/overlay/files.sha256
  { git -c safe.directory='*' -C "$REPO" show "$C:$f" 2>/dev/null || true; } | diff -u --label "a/$f" --label "b/$f" - "$R/harness/$f" >> $R/overlay/harness.diff || true
done
sha256sum $R/overlay/harness.diff
cat $R/overlay/files.sha256
grep -c '^[-+][^-+]' $R/overlay/harness.diff || true
