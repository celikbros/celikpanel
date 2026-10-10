#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set10-run; H=$R/harness-${1:-b}/deploy/e2e/release-recovery
A=/var/tmp/cp-upd1-build/20261010t184746z/upd1-artifacts.json
echo $A > $R/artifacts-fresh.path
for c in set10-debian13 set10-ubuntu set10-arch; do bash $H/run-set10.sh dry-run $c $A dry-$c > $R/build/dry-${1:-b}-$c.json 2> $R/build/dry-${1:-b}-$c.err; echo "dry $c rc=$?"; tail -n 3 $R/build/dry-${1:-b}-$c.err; done
python3 -I -c "import json,sys; d=json.load(open(sys.argv[1])); print({k: (d[k]['version'], d[k]['commit'][:12], d[k]['sha256'][:12]) for k in ('baseline','good')})" $A
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
