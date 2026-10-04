#!/bin/bash
# upd10: stage the build, run copy, tool records, the Debian probe and the cells into the repository evidence folder.
# Host only; read-only on the labs, the clone and the run copy. Refuses an existing folder.
# usage: stage10.sh SHORT:LAB:NODE:CELL:RUN:OVERLAY ...
set -u
R=/var/tmp/cp-upd10-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd10
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd10-20261001"
SRC=c67d18617178c0cc524298e5ff21c992779a1af2
[ -e "$S" ] && { echo "refusing: $S exists"; exit 2; }
mkdir -p "$S/build/cur" "$S/harness-run-copy/jobs" "$S/harness-run-copy/overlay" "$S/tools" "$S/host"
B=/var/tmp/cp-upd1-build/20261001t210302z; ART=$B/upd1-artifacts.json; CL=$B/repo; D="$S/build/cur"
cp -p $ART "$D/"
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d[sys.argv[2]]['dist_json'] if sys.argv[2] in d else '')" $ART $r)
  [ -n "$j" ] || continue
  cp -p $j "$D/dist-$r.json"
  grep -E 'acceptance guard|^go: ' $(dirname $j)/build.log > "$D/build-log-notes-$r.txt" 2>/dev/null || true
done
for x in start end rc out err; do cp -p $R/logs/build-cur.$x "$D/build.$x.txt"; done
G="git -c safe.directory=* -C $CL"
refs=$($G for-each-ref --format='%(refname)' refs/upd1/)
$G log --format='%H tree=%T parents=%P %s' $refs --no-walk > "$D/fixture-commits.txt"
{ echo "=== $SRC .. baseline (release policy label)"; $G diff $SRC refs/upd1/baseline;
  echo "=== baseline .. good (release policy label)"; $G diff refs/upd1/baseline refs/upd1/good;
  for r in $refs; do case $r in */baseline|*/good) ;; *) echo "=== good .. ${r#refs/upd1/}"; $G diff refs/upd1/good $r;; esac; done; } > "$D/fixture-patches.diff"
cp -p $R/build/* "$S/build/"
for f in $R/logs/offline-*.txt; do cp -p $f "$S/build/"; done
for x in start end rc out err; do [ -f $R/logs/offline-t1.$x ] && cp -p $R/logs/offline-t1.$x "$S/build/offline-t1.$x.txt"; [ -f $R/logs/offline-o1.$x ] && cp -p $R/logs/offline-o1.$x "$S/build/offline-o1-misstart.$x.txt"; done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-c67d1861-files.sha256 "$S/harness-run-copy/"
cp -p $R/overlay/* "$S/harness-run-copy/overlay/"
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
cp -p $P/setup-runcopy.sh $P/overlay.sh $P/bg.sh $P/job-build-cur.sh $P/job-offline.sh $P/job-offline-o1.sh $P/prove.sh $P/mkcell.sh $P/startcell.sh $P/pristine.sh "$S/harness-run-copy/"
for f in hostcheck.sh jobstat.sh waitjob.sh waitcell.sh peek.sh pkview.py pkv.sh stepview.py sv.sh showfail.sh probe-c-run1.sh probe-c.sh ext10.py scan10.py scan.sh stage10.sh sums.sh leftovers.sh pkcross.py pkctx.sh busyview.py findobs.sh attempts.sh setupsum.py ssum.sh termsum.py tsum.sh pkj.sh; do [ -f $P/$f ] && cp -p $P/$f "$S/tools/"; done
for f in hostcheck-before.txt c-drive.txt host-leftovers.txt packagekit-crosstab.txt packagekit-context.txt; do [ -f $P/$f ] && cp -p $P/$f "$S/host/"; done
for n in probe-c probe-c2 probe-c3; do for x in start end rc out err; do [ -f $R/logs/$n.$x ] && cp -p $R/logs/$n.$x "$S/host/debian13-$n.$x.txt"; done; done
for spec in "$@"; do
  IFS=: read -r short lab node cell run ov <<< "$spec"
  L=/var/tmp/cp-release-drill-$lab
  D="$S/$cell/$run"
  mkdir -p "$D/host" "$D/side"
  ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
  cp -a $ev/. "$D/"
  ( cd "$D" && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 && echo "$cell driver SHA256SUMS ok ($(wc -l < SHA256SUMS) entries)" || echo "$cell driver SHA256SUMS FAILED" )
  for x in start end rc out err; do cp -p $R/logs/cell-$short.$x "$D/host/wrapper.$x.txt" 2>/dev/null; done
  cp -p $R/jobs/job-cell-$short.sh "$D/host/job.sh"
  cp -p $R/jobs/job-cell-$short.harness "$D/host/harness.txt"
  echo "overlay=$ov" >> "$D/host/harness.txt"
  echo "lab=$L" > "$D/host/lab.txt"; basename $ev >> "$D/host/lab.txt"
  for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp -p "$f" "$D/host/"; done
  cp -p $L/lab.json $L/current-worker-baseline-$node-current-worker-baseline-result.json $L/current-worker-baseline-$node-current-worker-baseline.log "$D/host/" 2>/dev/null
  cp -p $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest "$D/host/" 2>/dev/null
  cp -p $L/cells/*/fixture-plan.json "$D/host/" 2>/dev/null
  python3 $P/ext10.py $lab > "$D/side/extract.txt" 2>&1
done
find "$S" -type f | wc -l; du -sh "$S"
