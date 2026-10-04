#!/bin/bash
S=/var/tmp/cp-upd3-run/stage/upd3-20261001
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
bash $P/origin-check.sh > $S/origin-check.txt
cp -p $P/origin-check.sh $P/checks2.sh $S/observer-tools/queries/
cd $S
echo "longest path (repo-relative):"
find . -type f | sed 's#^\./#deploy/e2e/release-recovery/evidence/upd3-20261001/#' | awk '{print length($0), $0}' | sort -rn | head -3
echo "native_evidence in every result.json:"; grep -h '"native_evidence"' */run-a/result.json | sort | uniq -c
echo "license steps:"; for f in */run-a/steps/05-license/step.json; do python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(sys.argv[1].split('/')[0], d['verdict'], json.dumps(d['checks'])[:300])" $f; done
