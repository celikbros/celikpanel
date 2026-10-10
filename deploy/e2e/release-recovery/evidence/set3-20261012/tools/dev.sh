#!/bin/bash
# set3: a mutable development copy (never used for a cell): the pristine archive plus the working tree's harness files;
# runs the named offline suites. usage: dev.sh test_a test_b ...
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set3-run; D=$R/harness-dev
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
[ -d $D ] || cp -a $R/harness-p $D
for f in "$REPO"/deploy/e2e/release-recovery/*.py "$REPO"/deploy/e2e/release-recovery/*.sh; do
  b=$(basename "$f"); sed 's/\r$//' "$f" > $D/deploy/e2e/release-recovery/$b.tmp
  cmp -s $D/deploy/e2e/release-recovery/$b.tmp $D/deploy/e2e/release-recovery/$b && rm $D/deploy/e2e/release-recovery/$b.tmp || { mv $D/deploy/e2e/release-recovery/$b.tmp $D/deploy/e2e/release-recovery/$b; echo "synced $b"; }
done
cd $D
for t in "$@"; do
  python3 -m unittest deploy/e2e/release-recovery/$t.py > $R/logs/dev-$t.txt 2>&1; r=$?
  echo "$t rc=$r $(grep -E '^Ran ' $R/logs/dev-$t.txt) $(tail -n 1 $R/logs/dev-$t.txt)"
  [ $r -eq 0 ] || grep -E '^(ERROR|FAIL)|Error|assert' $R/logs/dev-$t.txt | head -n 12
done
