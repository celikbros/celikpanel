#!/bin/bash
# upd13: stage the two builds, proofs, dry runs, offline logs, the run-copy record, jobs, scratch tools and host
# records into the repository evidence folder. Host only; read-only on the clones, labs and run copy. Run once.
set -u
R=/var/tmp/cp-upd13-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
SRC=f6cdd5a0c27ca865a2dc50239b106046e13739b7
TAG=bd14d97efc5cfd19acd70ddf0edb9c6343317e2b
[ -e "$S/build" ] && { echo "refusing: $S/build exists"; exit 2; }
mkdir -p "$S/build" "$S/harness-run-copy/jobs" "$S/harness-run-copy/overlay" "$S/harness-run-copy/overlay-h22" "$S/tools" "$S/host"
for spec in cur:20261002t203047z a80:20261002t203514z; do
  tag=${spec%%:*}; B=/var/tmp/cp-upd1-build/${spec#*:}; ART=$B/upd1-artifacts.json; CL=$B/repo; D="$S/build/$tag"
  mkdir -p "$D"
  cp -p $ART "$D/"
  for f in baseline-ref-proof.txt agent-deps.txt; do [ -f $B/$f ] && cp -p $B/$f "$D/"; done
  for r in baseline good defective startcheck realstart; do
    j=$(python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d[sys.argv[2]]['dist_json'] if sys.argv[2] in d else '')" $ART $r)
    [ -n "$j" ] || continue
    cp -p $j "$D/dist-$r.json"
    grep -E 'acceptance guard|^go: ' $(dirname $j)/build.log > "$D/build-log-notes-$r.txt" 2>/dev/null || true
  done
  for x in start end rc out err; do cp -p $R/logs/build-$tag.$x "$D/build.$x.txt"; done
  G="git -c safe.directory=* -C $CL"
  refs=$($G for-each-ref --format='%(refname)' refs/upd1/)
  $G log --format='%H tree=%T parents=%P %s' $refs --no-walk > "$D/fixture-commits.txt"
  if [ $tag = a80 ]; then
    { echo "=== tag v0.1.0-alpha.80 ($TAG) .. baseline (D-027 licence seam only)"; $G diff $TAG refs/upd1/baseline
      echo "=== source $SRC .. good (release policy label only)"; $G diff $SRC refs/upd1/good
      echo "=== good .. defective"; $G diff refs/upd1/good refs/upd1/defective; } > "$D/fixture-patches.diff"
  else
    { echo "=== $SRC .. baseline"; $G diff $SRC refs/upd1/baseline;
      echo "=== baseline .. good"; $G diff refs/upd1/baseline refs/upd1/good;
      for r in $refs; do case $r in */baseline|*/good) ;; *) echo "=== good .. ${r#refs/upd1/}"; $G diff refs/upd1/good $r;; esac; done; } > "$D/fixture-patches.diff"
  fi
  wc -l "$D/fixture-patches.diff"
done
cp -p $R/build/* "$S/build/"
for f in $R/logs/offline-*.txt; do cp -p $f "$S/build/"; done
for n in offline-p offline-h22 prove-all build-all; do
  for x in start end rc out err; do [ -f $R/logs/$n.$x ] && cp -p $R/logs/$n.$x "$S/build/$n.$x.txt"; done
done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-f6cdd5a0-files.sha256 "$S/harness-run-copy/"
cp -p $R/overlay/* "$S/harness-run-copy/overlay/"
cp -p $R/overlay-h22/* "$S/harness-run-copy/overlay-h22/"
cp -p $R/current-harness.txt "$S/harness-run-copy/" 2>/dev/null
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
for f in setup-runcopy.sh mkov.sh bg.sh job-build-cur.sh job-build-a80.sh job-build-all.sh go-build-all.sh go-build-cur.sh \
         go-build-a80.sh job-offline.sh job-offline-p.sh go-offline-p.sh prove.sh job-prove-cur.sh job-prove-a80.sh \
         job-prove-all.sh go-prove.sh artfiles.sh mkcell.sh go-cell.sh startcell.sh queue.sh go-queue.sh list-p1.txt \
         list-p2.txt list-all.txt overlay-h22.sh job-offline-h22.sh go-offline-h22.sh dry-h22.sh; do
  [ -f $P/$f ] && cp -p $P/$f "$S/harness-run-copy/"
done
for f in hostcheck.sh interop.sh jobstat.sh waitjob.sh waitcell.sh peek.sh qstat.sh offsum.sh provesum.sh cpaths.py ext13.py \
         dml13.py dml.sh summary13.py texts13.py pkview.py listen.sh labdu.sh stagecell13.sh stage-batch.sh rmoverlay13.sh \
         stagecommon13.sh cdrive.ps1 keepawake.ps1 scan.sh scan13.py sums.sh leftovers13.sh post13.sh dmltable13.py cpsum13.py \n         cells13.py gaps13.py dmlt.sh gaps.sh trysum.sh trycells.sh setpeek.sh overall.sh qwait.sh artfiles.sh; do
  [ -f $P/$f ] && cp -p $P/$f "$S/tools/"
done
for f in hostcheck-before.txt c-drive.txt removals.txt host-leftovers.txt keepawake.log sleep-events.txt; do [ -f $P/$f ] && cp -p $P/$f "$S/host/"; done
cp -p $R/logs/queue-*.out "$S/host/" 2>/dev/null
find "$S" -type f | wc -l; du -sh "$S"
