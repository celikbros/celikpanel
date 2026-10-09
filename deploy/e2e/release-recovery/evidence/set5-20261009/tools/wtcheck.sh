#!/bin/bash
# set5 read-only: are the working tree's harness files the files of run copy COPY (LF-normalized SHA-256)?
R=/var/tmp/cp-set5-run; copy=${1:-e}
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
out=$R/working-tree-against-copy-$copy.txt
{
  echo "working tree harness files against run copy $copy ($(date -u +%FT%TZ)); SHA-256 of the file with CR removed"
  while read -r sum path; do
    wt=$(sed 's/\r$//' "$REPO/$path" | sha256sum | cut -d' ' -f1)
    echo "$path copy=$sum working_tree=$wt $([ "$sum" = "$wt" ] && echo equal || echo DIFFERENT)"
  done < $R/overlay-$copy/files.sha256
  # (as first written this script also ran `git status` and `git diff --stat HEAD` over the Windows working tree from
  # WSL; that took minutes over drvfs and was stopped. Those two readings are taken with the Windows git instead and
  # are recorded by the session in host/working-tree-status.txt.)
  echo "HEAD $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD) branch $(git -c safe.directory='*' -C "$REPO" rev-parse --abbrev-ref HEAD)"
} > $out
cat $out
