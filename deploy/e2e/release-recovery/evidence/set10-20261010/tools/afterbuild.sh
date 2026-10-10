#!/bin/bash
# set10, after the builds: both artifact documents proven read-only on the host, the dry runs of the update cells
# (no lab), the offline suites of copies a and b.
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
R=/var/tmp/cp-set10-run; L=$R/logs
H=$R/harness-b/deploy/e2e/release-recovery
A81=$(tail -n 2 $L/build-a81.out | grep -m1 upd1-artifacts.json); echo "$A81" > $R/artifacts-a81.path
FR=$(cat $R/artifacts-fresh.path)
for n in fresh:$FR a81:$A81; do k=${n%%:*}; f=${n#*:}
  bash $H/run-upd1.sh prove "$f" > $R/build/$k-prove.json 2> $R/build/$k-prove.stderr.txt; r=$?
  echo "prove $k rc=$r"; echo "$(date -u +%FT%TZ) prove $k rc=$r" >> $R/progress.txt; done
for c in upd1-debian13-good upd1-debian13-defective upd1-ubuntu-good upd1-ubuntu-defective upd1-arch-good upd1-arch-defective; do
  bash $H/run-set10.sh dry-run $c "$A81" dry-$c > $R/build/dry-b-$c.json 2> $R/build/dry-b-$c.err; echo "dry $c rc=$?"; tail -n 2 $R/build/dry-b-$c.err; done
python3 -I -c "import json,sys; d=json.load(open(sys.argv[1])); print({k: (d[k]['version'], d[k]['commit'][:12], d[k]['sha256'][:12], d[k].get('reused_dist')) for k in ('baseline','good','defective')})" "$A81"
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
