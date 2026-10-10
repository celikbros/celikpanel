#!/bin/bash
# set6 read-only: the working tree's harness files of this run against the ones run copy COPY holds (the cells ran from
# the copy, not from the working tree). usage: wtcompare.sh COPY  -> /var/tmp/cp-set6-run/working-tree-against-copy-COPY.txt
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set6-run; copy=$1
W='/mnt/c/CELIKBROS PROJECTS/celikpanel'
out=$R/working-tree-against-copy-$copy.txt
{
  echo "read at $(date -u +%FT%TZ): the files of tools/files.txt in the working tree (bytes as they are on disk) against run copy $copy"
  same=0; n=0
  for b in $(tr -d '\r' < $J/files.txt); do
    f=deploy/e2e/release-recovery/$b; n=$((n + 1))
    a=$(sha256sum "$W/$f" | cut -c1-64); c=$(sha256sum "$R/harness-$copy/$f" | cut -c1-64)
    [ "$a" = "$c" ] && { same=$((same + 1)); echo "equal     $a  $f"; } || echo "DIFFERENT working tree $a copy $c  $f"
    grep -q $'\r' "$W/$f" && echo "          (the working-tree file holds a carriage return)"
  done
  echo "equal: $same of $n"
  cp $J/afterbuild-c.out.txt $R/logs/afterbuild-c.out.txt 2>/dev/null
} > $out
cat $out
