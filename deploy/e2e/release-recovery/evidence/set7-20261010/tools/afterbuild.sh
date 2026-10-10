#!/bin/bash
# set7, after the builds: the artifacts documents, the read-only host proof of each, and the dry runs (no lab).
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
R=/var/tmp/cp-set7-run; L=$R/logs
H=$R/harness-b/deploy/e2e/release-recovery
CUR=$(tail -n 2 $L/build-cur.out | grep -m1 upd1-artifacts.json)
A81=$(tail -n 2 $L/build-a81.out | grep -m1 upd1-artifacts.json)
[ -f "$CUR" ] && [ -f "$A81" ] || { echo "an artifacts document is missing"; exit 2; }
echo "$CUR" > $R/artifacts-cur.path; echo "$A81" > $R/artifacts-a81.path
mkdir -p $R/build
for b in cur a81; do
  art=$(cat $R/artifacts-$b.path)
  bash $H/run-upd1.sh prove "$art" > $R/build/$b-prove.json 2> $R/build/$b-prove.stderr.txt; r=$?
  echo "prove $b rc=$r"; echo "$(date -u +%FT%TZ) prove $b rc=$r" >> $R/progress.txt
  bash $H/run-set7.sh dry-run upd1-debian13-good "$art" dry-$b dry-$b > $R/build/dry-$b.json 2> $R/build/dry-$b.stderr.txt; r=$?
  echo "dry $b rc=$r"; echo "$r" > $R/build/dry-$b.rc.txt
  python3 -I -c "import json,sys; d=json.load(open(sys.argv[1])); print({k: (d[k]['version'], d[k]['commit'][:12], d[k]['sha256'][:12], bool(d[k].get('reused_dist'))) for k in ('baseline','good') })" "$art"
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
