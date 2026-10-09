#!/bin/bash
# set4b: the candidate source as a commit object that exists ONLY in a disposable clone of this run.
# The builder (build-upd1-artifacts.sh) checks out a commit, so the working tree's corrections need one. Nothing is
# committed to the working repository and its branch is not moved: the working repository is cloned to
# /var/tmp/cp-set4b-run/src-NAME, the files listed in candidate-files.txt (written on the Windows side from
# `git status`) are copied in from the working tree (CRLF normalized to LF), and ONE commit is made in that clone.
# usage: mk-src.sh NAME
set -euo pipefail
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set4b-run; name=$1
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
C=$R/src-$name
[ -e $C ] && { echo "refusing: $C exists"; exit 2; }
G() { git -c safe.directory='*' "$@"; }
base=$(G -C "$REPO" rev-parse 'HEAD^{commit}')
G clone --quiet --no-hardlinks "$REPO" $C
G -C $C checkout --quiet --detach $base
G -C $C config user.name "set4b disposable candidate"
G -C $C config user.email "set4b-candidate@example.invalid"
: > $R/src-$name-files.sha256
while IFS= read -r f; do
  f=$(echo "$f" | tr -d '\r'); [ -n "$f" ] || continue
  [ -f "$REPO/$f" ] || { echo "listed file is missing: $f"; exit 3; }
  mkdir -p "$C/$(dirname "$f")"
  if grep -qI . "$REPO/$f"; then sed 's/\r$//' "$REPO/$f" > "$C/$f"; else cp "$REPO/$f" "$C/$f"; fi
  case $f in *.sh) chmod 0755 "$C/$f";; esac
  G -C $C add -- "$f"
  ( cd $C && sha256sum "$f" ) >> $R/src-$name-files.sha256
done < $J/candidate-files.txt
G -C $C commit --quiet -m "set4b candidate: $base + the working tree's corrections of 2026-10-09 (disposable; this commit exists only in this clone)"
commit=$(G -C $C rev-parse HEAD)
{
  echo "base=$base"
  echo "base_tree=$(G -C $C rev-parse "$base^{tree}")"
  echo "candidate=$commit"
  echo "candidate_tree=$(G -C $C rev-parse "$commit^{tree}")"
  echo "clone=$C"
  echo "files=$(wc -l < $R/src-$name-files.sha256)"
} > $R/src-$name.txt
G -C $C diff --stat $base $commit | tail -n 1 >> $R/src-$name.txt
G -C $C diff $base $commit > $R/src-$name.diff
G -C $C diff --name-only $base $commit | LC_ALL=C sort > $R/src-$name-changed.txt
diff <(tr -d '\r' < $J/candidate-files.txt | grep -v '^$' | LC_ALL=C sort) $R/src-$name-changed.txt > $R/src-$name-listed-vs-committed.txt && echo "the commit changes exactly the listed files" || { echo "LISTED AND COMMITTED FILES DIFFER"; cat $R/src-$name-listed-vs-committed.txt; }
[ -z "$(G -C $C status --porcelain)" ] && echo "clone clean after the commit"
cat $R/src-$name.txt
echo "$(date -u +%FT%TZ) made candidate source $name $commit" >> $R/progress.txt
