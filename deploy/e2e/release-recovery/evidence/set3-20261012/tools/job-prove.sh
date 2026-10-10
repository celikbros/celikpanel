#!/bin/bash
# set3: read-only host proof of every archive of both builds, then the dry runs (no guest, no lab).
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set3-run; H=$R/harness-a/deploy/e2e/release-recovery
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
rc=0
for tag in cur a81; do
  ART=$(tr -d '\r\n' < $J/art-$tag.txt)
  bash $H/run-upd1.sh prove "$ART" > $R/build/$tag-prove.json 2> $R/build/$tag-prove.stderr.txt; r=$?
  echo "prove $tag rc=$r"; [ $r -eq 0 ] || { rc=1; tail -n 3 $R/build/$tag-prove.stderr.txt; }
done
ART=$(tr -d '\r\n' < $J/art-cur.txt)
for c in set3-debian13 set3-ubuntu rid3-debian13 rid3-ubuntu rid3-arch; do
  bash $H/run-set2.sh dry-run $c "$ART" dry-$c > $R/build/set3-dry-$c.json 2> $R/build/set3-dry-$c.stderr.txt; r=$?
  echo "dry $c rc=$r native_evidence=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("native_evidence"))' $R/build/set3-dry-$c.json 2>&1)"; [ $r -eq 0 ] || rc=1
done
ART=$(tr -d '\r\n' < $J/art-a81.txt)
for c in upd1-debian13-good upd1-debian13-defective upd1-debian13-startcheck upd1-debian13-owner-continuation upd1-debian13-mgmt-off-reboot upd1-ubuntu-good upd1-ubuntu-defective upd1-ubuntu-owner-continuation upd1-arch-good upd1-arch-defective; do
  bash $H/run-upd1.sh dry-run $c "$ART" dry-$c > $R/build/a81-dry-$c.json 2> $R/build/a81-dry-$c.stderr.txt; r=$?
  echo "dry a81 $c rc=$r $(tail -n 1 $R/build/a81-dry-$c.stderr.txt | cut -c1-160)"
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null && echo "A DRY RUN CREATED A LAB" || echo "no dry-run lab exists"
exit $rc
