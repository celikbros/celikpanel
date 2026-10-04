#!/bin/bash
R=/var/tmp/cp-upd13-run
export PYTHONDONTWRITEBYTECODE=1
cd $R/harness-h22
for c in upd1-arch-owner-continuation upd1-ubuntu-good; do
  bash deploy/e2e/release-recovery/run-upd1.sh dry-run $c $(cat $R/art-cur.txt) upd13-dry > $R/build/h22-dry-run-$c.json 2> $R/build/h22-dry-run-$c.stderr.txt; echo "dry $c rc=$?"
  python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d.get('native_evidence'),[s['name'] for s in d['steps']])" $R/build/h22-dry-run-$c.json
done
ls -d /var/tmp/cp-release-drill-upd13-dry 2>/dev/null && echo "WARNING lab" || echo "no dry-run lab created"
echo harness-h22 > $R/current-harness.txt; cat $R/current-harness.txt
