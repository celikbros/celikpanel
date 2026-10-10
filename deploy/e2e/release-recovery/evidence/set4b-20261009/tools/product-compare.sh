#!/bin/bash
# set4b: every file of the working tree that differs from HEAD (worktree-files.txt, from `git status` on the Windows
# side at the end of the run), compared with the candidate commit of the disposable clone (CRLF normalized).
R=/var/tmp/cp-set4b-run; C=$R/src-c1
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
echo "# $(date -u +%FT%TZ) working tree (HEAD $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD)) against the candidate $(sed -n 's/^candidate=//p' $R/src-c1.txt)"
same=0; differ=0; absent=0
while IFS= read -r f; do
  f=$(echo "$f" | tr -d '\015'); [ -n "$f" ] || continue
  w=$(sed 's/\x0d$//' "$REPO/$f" | sha256sum | cut -d' ' -f1)
  if [ -f "$C/$f" ]; then
    c=$(sha256sum "$C/$f" | cut -d' ' -f1)
    if [ "$w" = "$c" ]; then echo "same      $f"; same=$((same+1)); else echo "DIFFERENT $f"; differ=$((differ+1)); fi
  else echo "not in the candidate (as at HEAD there, or new since) $f"; absent=$((absent+1)); fi
done < $J/worktree-files.txt
echo "# same=$same different=$differ not-in-candidate=$absent"
echo "# product files (cmd/, internal/, web/src) that differ from the candidate:"
