#!/bin/bash
# set1: a mutable development copy (never used for a cell): git archive c4cf7fd9 + the harness files; runs the new
# offline test and a dry run.
set -u
R=/var/tmp/cp-set1-run
H=$R/harness-dev
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set1
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
if [ ! -d $H ]; then mkdir -p $H; git -c safe.directory='*' -C "$REPO" archive c4cf7fd9 | tar -x -C $H; fi
while read -r b; do
  [ -n "$b" ] || continue
  f=deploy/e2e/release-recovery/$b
  sed 's/\r$//' "$REPO/$f" > "$H/$f"
done < $P/files.txt
export PYTHONDONTWRITEBYTECODE=1
cd $H
python3 -m unittest deploy/e2e/release-recovery/test_settings_writes_trial.py 2>&1 | tail -n 25
ART=$(cat $R/art-cur.txt 2>/dev/null)
if [ -n "$ART" ]; then
  for c in set1-debian13 set1-ubuntu set1-arch; do
    bash deploy/e2e/release-recovery/run-set1.sh dry-run $c $ART set1-dry > /tmp/set1-dry-$c.json 2> /tmp/set1-dry-$c.err; echo "dry $c rc=$? $(python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d['native_evidence'],d['candidate']['version'],len(d['steps']))" /tmp/set1-dry-$c.json 2>&1 | tail -1) $(tail -n 1 /tmp/set1-dry-$c.err)"
  done
fi
ls -d /var/tmp/cp-release-drill-set1-dry 2>/dev/null && echo "WARNING: dry-run created a lab" || echo "no dry-run lab created"
