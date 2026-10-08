#!/bin/bash
# set1: a run copy = git archive c4cf7fd9 + exactly the listed harness files from the working tree (CRLF normalized
# to LF), each listed with its SHA-256, and the diff against c4cf7fd9 (a new file diffs against /dev/null).
# usage: mkov.sh NAME FILE...   (FILE relative to deploy/e2e/release-recovery) -> $R/harness-NAME, $R/overlay-NAME
set -euo pipefail
R=/var/tmp/cp-set1-run
C=c4cf7fd9
name=$1; shift
H=$R/harness-$name
O=$R/overlay-$name
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
[ -e $H ] && { echo "refusing: $H exists"; exit 2; }
mkdir -p $H $O
git -c safe.directory='*' -C "$REPO" archive $C | tar -x -C $H
: > $O/files.sha256; : > $O/harness.diff
for b in "$@"; do
  f=deploy/e2e/release-recovery/$b
  mode=0644; [ -x "$H/$f" ] && mode=0755
  case $b in *.sh) mode=0755;; esac
  sed 's/\r$//' "$REPO/$f" > "$H/$f.tmp"; install -m $mode "$H/$f.tmp" "$H/$f"; rm -f "$H/$f.tmp"
  sha256sum "$H/$f" | sed "s#$H/##" >> $O/files.sha256
  if git -c safe.directory='*' -C "$REPO" cat-file -e "$C:$f" 2>/dev/null; then
    git -c safe.directory='*' -C "$REPO" show "$C:$f" | diff -u --label "a/$f" --label "b/$f" - "$H/$f" >> $O/harness.diff || true
  else
    diff -u --label /dev/null --label "b/$f" /dev/null "$H/$f" >> $O/harness.diff || true
  fi
done
sha256sum $O/harness.diff; cat $O/files.sha256; grep -c '^[-+][^-+]' $O/harness.diff
# every other file of the copy is the archive's
( cd $H && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > $O/runcopy-files.sha256
diff <(cat $R/runcopy-$C-files.sha256) $O/runcopy-files.sha256 | grep '^[<>]' | sed 's/^\(.\) [0-9a-f]*  /\1 /' | sort | uniq -c | sed 's/^/differs: /' > $O/differs-from-archive.txt || true
cat $O/differs-from-archive.txt
