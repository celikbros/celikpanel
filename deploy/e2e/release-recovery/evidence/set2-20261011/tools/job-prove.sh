#!/bin/bash
# set2: read-only host proof of every archive of the build (no guest).
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set2-run
ART=$(cat $R/art-cur.txt)
bash $R/harness-p/deploy/e2e/release-recovery/run-upd1.sh prove "$ART" > $R/build/cur-prove.json 2> $R/build/cur-prove.stderr.txt; rc=$?
echo "prove rc=$rc"; tail -n 3 $R/build/cur-prove.stderr.txt
python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d.get('native_evidence'), sorted(d['proofs']))" $R/build/cur-prove.json
exit $rc
