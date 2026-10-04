#!/bin/bash
# upd9: redo build/cur fixture commits and patches with the clone's real ref names (stage9 guessed startcheck/realstart).
set -eu
CL=/var/tmp/cp-upd1-build/20261001t174240z/repo
D="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd9-20261001/build/cur"
G="git -c safe.directory=* -C $CL"
$G for-each-ref --format='%(refname)' refs/upd1/
refs=$($G for-each-ref --format='%(refname)' refs/upd1/)
$G log --format='%H tree=%T parents=%P %s' $refs --no-walk > "$D/fixture-commits.txt"
{ echo "=== efcba145 .. baseline (release policy label)"; $G diff efcba145543155b9c4104f9d42538a685cfab17d refs/upd1/baseline;
  echo "=== baseline .. good (release policy label)"; $G diff refs/upd1/baseline refs/upd1/good;
  echo "=== good .. defective"; $G diff refs/upd1/good refs/upd1/defective;
  for r in $refs; do case $r in */baseline|*/good|*/defective) ;; *) echo "=== good .. ${r#refs/upd1/}"; $G diff refs/upd1/good $r;; esac; done; } > "$D/fixture-patches.diff"
grep -n '^===' "$D/fixture-patches.diff"; wc -l "$D/fixture-commits.txt"
