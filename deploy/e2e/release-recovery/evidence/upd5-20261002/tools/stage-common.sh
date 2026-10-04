#!/bin/bash
# Stage build, run-copy and tool records into the upd5 stage (host only, read-only on labs and the clone).
set -u
R=/var/tmp/cp-upd5-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd5run
S=$R/stage/upd5-20261002
mkdir -p $S/build $S/harness-run-copy/jobs $S/tools $S/host
ART=$(cat $R/ART); CL=$(dirname $ART)/repo
cp -p $ART $S/build/
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]]['dist_json'])" $ART $r)
  cp -p $j $S/build/dist-$r.json
done
cp -p $R/build/* $S/build/
for x in start end rc out err; do cp -p $R/logs/build.$x $S/build/build.$x.txt; cp -p $R/logs/offline.$x $S/build/offline-job.$x.txt; done
cp -p $R/logs/offline-owner.txt $S/build/offline-tests-owner-6cda60b8.txt
cp -p $R/logs/offline-archive.txt $S/build/offline-tests-archive-6cda60b8.txt
cp -p $R/harness-commit.txt $S/build/source-commit.txt
git -c safe.directory='*' -C $CL log --format='%H tree=%T parents=%P %s' refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start --no-walk > $S/build/fixture-commits.txt
for c in $(git -c safe.directory='*' -C $CL rev-parse refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start); do
  echo "=== 6cda60b8..$c"; git -c safe.directory='*' -C $CL diff --stat 6cda60b87e8144ae33aaa343d04a9c3923d8755e $c
done > $S/build/fixture-commits-stat.txt
{ echo "=== start-check over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/start-check; echo "=== real-start over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/real-start; echo "=== defective over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/defective; } > $S/build/fixture-kind-patches.diff
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > $S/build/go-version.txt
cp -p $R/runcopy-files.sha256 $S/harness-run-copy/runcopy-6cda60b8-files.sha256
cp -p $R/jobs/* $S/harness-run-copy/jobs/
cp -p $R/H16-owner_update_trial.py.diff $R/runcopy-h16-files.sha256 $S/harness-run-copy/
cp -p $R/logs/offline-owner-h16.txt $S/harness-run-copy/offline-tests-owner-h16.txt
cp -p $P/mk-h16.py $P/do-h16.sh $S/harness-run-copy/
cp -p $P/setup-runcopy.sh $P/bg.sh $P/job-build.sh $P/job-offline.sh $P/mkjobs.sh $P/setart.sh $P/prove-dry.sh $P/startcell.sh $S/harness-run-copy/
cp -p $P/hostcheck.sh $P/snapshot-host.sh $P/jobstat.sh $P/waitjob.sh $P/peek.sh $P/stagecell.sh $P/stage-common.sh $P/ext5.py $P/summary5.py $P/scan5.py $P/views.py $P/live.py $P/failpeek.sh $P/failpeek2.sh $P/reext.sh $S/tools/
cp -p $P/hostcheck-before.txt $P/host-df-before.txt $S/host/
find $S -maxdepth 2 -type d | sort; find $S -type f | wc -l; du -sh $S
