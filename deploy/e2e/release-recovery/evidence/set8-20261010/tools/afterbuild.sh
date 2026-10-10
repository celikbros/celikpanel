#!/bin/bash
# set8, after the build: the artifacts document, the read-only host proof, and the dry runs of the three cells (no lab).
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
R=/var/tmp/cp-set8-run; L=$R/logs
H=$R/harness-c/deploy/e2e/release-recovery
CUR=$(tail -n 2 $L/build-cur.out | grep -m1 upd1-artifacts.json)
[ -f "$CUR" ] || { echo "the artifacts document is missing"; exit 2; }
echo "$CUR" > $R/artifacts-cur.path
mkdir -p $R/build
bash $H/run-upd1.sh prove "$CUR" > $R/build/cur-prove.json 2> $R/build/cur-prove.stderr.txt; r=$?
echo "prove cur rc=$r"; echo "$(date -u +%FT%TZ) prove cur rc=$r" >> $R/progress.txt
for c in set8-debian13 set8-ubuntu set8-arch; do
  bash $H/run-set8.sh dry-run $c "$CUR" dry-$c > $R/build/dry-$c.json 2> $R/build/dry-$c.stderr.txt; r=$?
  echo "dry $c rc=$r"; echo "$r" > $R/build/dry-$c.rc.txt
done
python3 -I -c "import json,sys; d=json.load(open(sys.argv[1])); print({k: (d[k]['version'], d[k]['commit'][:12], d[k]['sha256'][:12]) for k in ('baseline','good')})" "$CUR"
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
