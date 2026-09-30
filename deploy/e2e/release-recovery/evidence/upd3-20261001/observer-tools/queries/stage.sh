#!/bin/bash
# Stage the upd3 evidence (host only, read-only on labs). Output: /var/tmp/cp-upd3-run/stage/upd3-20261001
set -u
R=/var/tmp/cp-upd3-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
S=$R/stage/upd3-20261001
[ -e $S ] && { echo "refusing: stage exists"; exit 2; }
mkdir -p $S/build $S/harness-run-copy/jobs $S/observer-tools/v1 $S/observer-tools/queries
ART=$(cat $R/ART); B=$(dirname $ART); CL=$B/repo
# build
cp -p $ART $S/build/
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json;print(json.load(open('$ART'))['$r']['dist_json'])")
  cp -p $j $S/build/dist-$r.json
done
cp -p $R/build/* $S/build/
for x in start end rc out err; do cp -p $R/logs/build.$x $S/build/build.$x.txt; done
cp -p $R/logs/offline-owner.txt $S/build/offline-tests-owner-94be6b6e.txt
cp -p $R/logs/offline-archive.txt $S/build/offline-tests-archive-94be6b6e.txt
for x in start end rc out err; do cp -p $R/logs/offline.$x $S/build/offline-job.$x.txt; done
cp -p $R/harness-commit.txt $S/build/source-commit.txt
git -C $CL log --format='%H tree=%T parents=%P %s' refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start --no-walk > $S/build/fixture-commits.txt
for c in $(git -C $CL rev-parse refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start); do
  echo "=== 94be6b6e..$c"; git -C $CL diff --stat 94be6b6ec3c50929bd8119fadb0c8c0879ae8dbe $c
done > $S/build/fixture-commits-stat.txt
{ echo "=== start-check over good"; git -C $CL diff refs/upd1/good refs/upd1/start-check; echo "=== real-start over good"; git -C $CL diff refs/upd1/good refs/upd1/real-start; echo "=== defective over good"; git -C $CL diff refs/upd1/good refs/upd1/defective; } > $S/build/fixture-kind-patches.diff
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > $S/build/go-version.txt
# harness run copy
cp -p $R/H8-owner_update_trial.py.diff $S/harness-run-copy/
cp -p $R/runcopy-files.sha256 $S/harness-run-copy/runcopy-94be6b6e-files.sha256
cp -p $R/runcopy-h8-files.sha256 $S/harness-run-copy/runcopy-h8-files.sha256
cp -p $R/logs/offline-owner-h8.txt $S/harness-run-copy/offline-tests-owner-h8.txt
cp -p $R/jobs/* $S/harness-run-copy/jobs/
cp -p $P/mk-h8.py $P/do-h8.sh $P/job-build.sh $P/job-offline.sh $P/setup-runcopy.sh $S/harness-run-copy/
# observer tools
cp -p $R/tools/*.py $R/tools/*.sh $S/observer-tools/
cp -p $R/tools/v1/* $S/observer-tools/v1/
cp -p $P/q-*.sh $P/summary.py $P/timers.py $P/scan.py $P/stage.sh $P/lslab.sh $P/hostcheck.sh $P/prove.sh $P/install-tools.sh $P/install-tools2.sh $S/observer-tools/queries/ 2>/dev/null
cp -p $R/summary.txt $S/summary-per-cell.txt
# cells
for pair in d13good:upd3-d13-good-a:debian13:upd1-debian13-good d13def:upd3-d13-def-a:debian13:upd1-debian13-defective \
            d13sc:upd3-d13-sc-a:debian13:upd1-debian13-startcheck d13rs:upd3-d13-rs-a:debian13:upd1-debian13-realstart \
            archgood:upd3-arch-good-a:arch:upd1-arch-good archdef:upd3-arch-def-a:arch:upd1-arch-defective \
            archsc:upd3-arch-sc-a:arch:upd1-arch-startcheck archrs:upd3-arch-rs-a:arch:upd1-arch-realstart; do
  IFS=: read short lab node cell <<< "$pair"
  L=/var/tmp/cp-release-drill-$lab
  D=$S/$cell/run-a
  mkdir -p $D/host $D/side
  ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
  cp -a $ev/. $D/
  ( cd $D && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 && echo "$cell driver SHA256SUMS ok" || echo "$cell driver SHA256SUMS FAILED" )
  for x in start end rc out err; do cp -p $R/logs/cell-$short.$x $D/host/wrapper.$x.txt; cp -p $R/logs/side-$short.$x $D/host/sidecar-job.$x.txt 2>/dev/null; done
  cp -p $R/jobs/job-cell-$short.harness $D/host/harness.txt 2>/dev/null || echo "harness=harness (94be6b6e run copy, before H8)" > $D/host/harness.txt
  echo "lab=$L" > $D/host/lab.txt; basename $ev >> $D/host/lab.txt
  for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp -p "$f" $D/host/; done
  cp -p $L/lab.json $L/current-worker-baseline-$node-current-worker-baseline-result.json $L/current-worker-baseline-$node-current-worker-baseline.log $D/host/ 2>/dev/null
  cp -p $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest $D/host/ 2>/dev/null
  cp -p $L/cells/*/fixture-plan.json $D/host/ 2>/dev/null
  cp -p $R/side/cell-$short/* $D/side/
done
find $S -type f | wc -l; du -sh $S
