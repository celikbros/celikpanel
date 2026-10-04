#!/bin/bash
# upd6: stage build, run-copy, tool records and the one cell straight into the repository evidence folder.
# Host only; read-only on the lab, the clone and the run copy. Refuses an existing folder.
set -u
R=/var/tmp/cp-upd6-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd6run
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd6-20261002"
[ -e "$S" ] && { echo "refusing: $S exists"; exit 2; }
mkdir -p "$S/build" "$S/harness-run-copy/jobs" "$S/tools" "$S/host"
ART=$(cat $R/ART); CL=$(dirname $ART)/repo
cp -p $ART "$S/build/"
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]]['dist_json'])" $ART $r)
  cp -p $j "$S/build/dist-$r.json"
done
cp -p $R/build/* "$S/build/"
for x in start end rc out err; do cp -p $R/logs/build.$x "$S/build/build.$x.txt"; cp -p $R/logs/offline.$x "$S/build/offline-job.$x.txt"; done
cp -p $R/logs/offline-owner.txt "$S/build/offline-tests-owner-6cda60b8.txt"
cp -p $R/logs/offline-archive.txt "$S/build/offline-tests-archive-6cda60b8.txt"
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
G="git -c safe.directory=* -C $CL"
$G log --format='%H tree=%T parents=%P %s' refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start --no-walk > "$S/build/fixture-commits.txt"
for c in $($G rev-parse refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start); do
  echo "=== 6cda60b8..$c"; $G diff --stat 6cda60b87e8144ae33aaa343d04a9c3923d8755e $c
done > "$S/build/fixture-commits-stat.txt"
{ echo "=== start-check over good"; $G diff refs/upd1/good refs/upd1/start-check; echo "=== real-start over good"; $G diff refs/upd1/good refs/upd1/real-start; echo "=== defective over good"; $G diff refs/upd1/good refs/upd1/defective; } > "$S/build/fixture-kind-patches.diff"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-files.sha256 "$S/harness-run-copy/runcopy-6cda60b8-files.sha256"
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
cp -p $P/setup-runcopy.sh $P/bg.sh $P/job-build.sh $P/job-offline.sh $P/setart.sh $P/startcell.sh "$S/harness-run-copy/"
cp -p $P/hostcheck.sh $P/snapshot-host.sh $P/jobstat.sh $P/waitjob.sh $P/peek.sh $P/waitpeek.sh $P/stage6.sh $P/ext6.py $P/scan6.py $P/scan.sh "$S/tools/"
cp -p $P/hostcheck-before.txt $P/host-df-before.txt $P/host-df-after.txt $P/c-drive.txt $P/host-leftovers.txt "$S/host/"
# the cell
short=archoc; lab=upd6-arch-oc-a; node=arch; cell=upd1-arch-owner-continuation; run=run-a
L=/var/tmp/cp-release-drill-$lab
D="$S/$cell/$run"
mkdir -p "$D/host" "$D/side"
ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
cp -a $ev/. "$D/"
( cd "$D" && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 && echo "$cell driver SHA256SUMS ok ($(wc -l < SHA256SUMS) entries)" || echo "$cell driver SHA256SUMS FAILED" )
for x in start end rc out err; do cp -p $R/logs/cell-$short.$x "$D/host/wrapper.$x.txt" 2>/dev/null; done
cp -p $R/jobs/job-cell-$short.sh "$D/host/job.sh"
cp -p $R/jobs/job-cell-$short.harness "$D/host/harness.txt"
echo "lab=$L" > "$D/host/lab.txt"; basename $ev >> "$D/host/lab.txt"
for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp -p "$f" "$D/host/"; done
cp -p $L/lab.json $L/current-worker-baseline-$node-current-worker-baseline-result.json $L/current-worker-baseline-$node-current-worker-baseline.log "$D/host/" 2>/dev/null
cp -p $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest "$D/host/" 2>/dev/null
cp -p $L/cells/*/fixture-plan.json "$D/host/" 2>/dev/null
cp -p $P/extract-archoc.txt "$D/side/extract.txt"
find "$S" -type f | wc -l; du -sh "$S"
