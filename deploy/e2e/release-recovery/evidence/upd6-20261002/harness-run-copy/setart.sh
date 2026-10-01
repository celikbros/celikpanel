#!/bin/bash
# upd6: record the artifacts document, write the single cell job, run prove + one dry run.
set -eu
R=/var/tmp/cp-upd6-run
echo /var/tmp/cp-upd1-build/20261001t092830z/upd1-artifacts.json > $R/ART
ART=$(cat $R/ART)
python3 - $ART <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("source_head", d["source_head"])
for r in ("baseline", "good", "defective", "startcheck", "realstart"):
    e = d[r]
    print(r, e["version"], e["sequence"], e["commit"], e["tree"], e["sha256"], e["license_mode"], e.get("parent"))
PY
cat > $R/jobs/job-cell-archoc.sh <<EOT
#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash $R/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-arch-owner-continuation $ART upd6-arch-oc-a 2611
EOT
echo "harness=$R/harness (git archive 6cda60b8)" > $R/jobs/job-cell-archoc.harness
cat $R/jobs/job-cell-archoc.sh
export PYTHONDONTWRITEBYTECODE=1
W=$R/harness/deploy/e2e/release-recovery/run-upd1.sh
cd $R/harness
set +e
bash $W prove "$ART" > $R/build/prove.json 2> $R/build/prove.stderr.txt
echo $? > $R/build/prove.rc
echo "prove rc=$(cat $R/build/prove.rc)"
c=upd1-arch-owner-continuation
bash $W dry-run $c "$ART" upd6-dry > $R/build/dry-run-$c.json 2> $R/build/dry-run-$c.stderr.txt
echo $? > $R/build/dry-run-$c.rc
python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print('native_evidence=',d.get('native_evidence'),'port_hold=',json.dumps(d.get('port_hold')))" $R/build/dry-run-$c.json
echo "dry-run rc=$(cat $R/build/dry-run-$c.rc)"
ls -d /var/tmp/cp-release-drill-upd6-dry 2>/dev/null && echo "WARNING: dry-run created a lab" || echo "no dry-run lab created"
find $R/harness -name __pycache__ | head -3
