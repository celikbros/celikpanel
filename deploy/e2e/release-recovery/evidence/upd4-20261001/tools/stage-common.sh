#!/bin/bash
# Stage build, run-copy and tool records into the upd4 stage (host only, read-only on labs and the clone).
set -u
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
S=$R/stage/upd4-20261001
mkdir -p $S/build $S/harness-run-copy/jobs $S/tools
ART=$(cat $R/ART); CL=$(dirname $ART)/repo
cp -p $ART $S/build/
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]]['dist_json'])" $ART $r)
  cp -p $j $S/build/dist-$r.json
done
cp -p $R/build/* $S/build/
for x in start end rc out err; do cp -p $R/logs/build.$x $S/build/build.$x.txt; cp -p $R/logs/offline.$x $S/build/offline-job.$x.txt; done
cp -p $R/logs/offline-owner.txt $S/build/offline-tests-owner-a6dd5b1e.txt
cp -p $R/logs/offline-archive.txt $S/build/offline-tests-archive-a6dd5b1e.txt
cp -p $R/harness-commit.txt $S/build/source-commit.txt
git -c safe.directory='*' -C $CL log --format='%H tree=%T parents=%P %s' refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start --no-walk > $S/build/fixture-commits.txt
for c in $(git -c safe.directory='*' -C $CL rev-parse refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start); do
  echo "=== a6dd5b1e..$c"; git -c safe.directory='*' -C $CL diff --stat a6dd5b1ec66c142d5b7be1b8181fe61fb23cfc70 $c
done > $S/build/fixture-commits-stat.txt
{ echo "=== start-check over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/start-check; echo "=== real-start over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/real-start; echo "=== defective over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/defective; } > $S/build/fixture-kind-patches.diff
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > $S/build/go-version.txt
# harness run copies
cp -p $R/runcopy-files.sha256 $S/harness-run-copy/runcopy-a6dd5b1e-files.sha256
cp -p $R/runcopy-h12-files.sha256 $S/harness-run-copy/runcopy-h12-files.sha256
cp -p $R/H12-H13-owner_update_trial.py.diff $S/harness-run-copy/
cp -p $R/logs/offline-owner-h12.txt $S/harness-run-copy/offline-tests-owner-h12.txt
cp -p $R/runcopy-h14-files.sha256 $S/harness-run-copy/runcopy-h14-files.sha256
cp -p $R/H14-owner_update_trial.py.diff $R/H12-H13-H14-cumulative-owner_update_trial.py.diff $S/harness-run-copy/
cp -p $R/logs/offline-owner-h14.txt $S/harness-run-copy/offline-tests-owner-h14.txt
cp -p $P/mk-h14.py $P/do-h14.sh $P/job-h14.sh $P/check-h14.sh $S/harness-run-copy/
cp -p $R/jobs/* $S/harness-run-copy/jobs/
cp -p $P/setup-runcopy.sh $P/bg.sh $P/job-build.sh $P/job-offline.sh $P/mkjobs.sh $P/mkjobs-h12.sh $P/mk-h12.py $P/do-h12.sh $P/prove-dry.sh $P/startcell.sh $S/harness-run-copy/
# report tools (host-only readers)
cp -p $P/hostcheck.sh $P/jobstat.sh $P/waitjob.sh $P/peek.sh $P/stagecell.sh $P/stage-common.sh $P/ext4.py $P/summary4.py $P/scan4.py \
      $P/h11texts.py $P/gsamples.py $P/jgrep.sh $P/lines.sh $P/sample.py $P/inspects.py $P/apis.py $P/mgmtsteps.py $P/lsstage.sh $P/pull.sh $S/tools/
cp -p $R/h11-product-screen-texts.txt $S/h11-product-screen-texts.txt
find $S -maxdepth 2 -type d | sort; find $S -type f | wc -l; du -sh $S
