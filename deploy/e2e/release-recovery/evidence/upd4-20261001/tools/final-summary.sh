#!/bin/bash
set -u
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
S=$R/stage/upd4-20261001
python3 $P/summary4.py $S $S > /dev/null
mkdir -p $P/pulled
cp $S/summary-per-cell.txt $S/outage-windows.txt $S/step-table.md $P/pulled/
# origin checks and licence (from each result scope and license step)
python3 - $S > $S/origin-check.txt <<'PY'
import glob, json, sys
S = sys.argv[1]
for run in sorted(glob.glob(S + "/upd1-*/run-*")):
    r = json.load(open(run + "/result.json"))
    origin = (r.get("scope") or {}).get("origin")
    lic = sorted(glob.glob(run + "/steps/*-license/step.json"))
    lc = json.load(open(lic[0])).get("checks", {}) if lic else {}
    flat = json.dumps(lc)
    print(run.split(S + "/")[1], "origin:", json.dumps(origin))
    print("   license step: not-contacted mentions:", flat.count("not contacted"), "keys:", sorted(lc)[:10])
PY
cp $S/origin-check.txt $P/pulled/
pgrep -a qemu || echo "no qemu process"
ls -d /var/tmp/cp-release-drill-upd4* /var/tmp/cp-upd4*
du -sh /var/tmp/cp-release-drill-upd4* /var/tmp/cp-upd4-run /var/tmp/cp-upd1-build/20260930t221920z 2>/dev/null
ART=$(cat $R/ART)
python3 -c "import json,sys;d=json.load(open(sys.argv[1]));[print('/var/tmp/cp-pair-accept/dist/'+d[r]['commit']+'-acceptance-license') for r in ('baseline','good','defective','startcheck','realstart')]" $ART
