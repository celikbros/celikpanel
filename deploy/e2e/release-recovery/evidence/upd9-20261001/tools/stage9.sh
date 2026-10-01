#!/bin/bash
# upd9: stage builds, run-copy, tool records, the probe and the cells into the repository evidence folder.
# Host only; read-only on the labs, the clones and the run copy. Refuses an existing folder.
# usage: stage9.sh SHORT:LAB:NODE:CELL:RUN:OVERLAYDIR ...
set -u
R=/var/tmp/cp-upd9-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd9
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd9-20261001"
[ -e "$S" ] && { echo "refusing: $S exists"; exit 2; }
mkdir -p "$S/build" "$S/harness-run-copy/jobs" "$S/tools" "$S/host"
for b in cur:20261001t174240z a80:20261001t174610z; do
  tag=${b%%:*}; B=/var/tmp/cp-upd1-build/${b#*:}; ART=$B/upd1-artifacts.json; CL=$B/repo
  D="$S/build/$tag"; mkdir -p "$D"
  cp -p $ART "$D/"
  [ -f $B/baseline-ref-proof.txt ] && cp -p $B/baseline-ref-proof.txt "$D/"
  [ -f $B/agent-deps.txt ] && cp -p $B/agent-deps.txt "$D/"
  for r in baseline good defective startcheck realstart; do
    j=$(python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d[sys.argv[2]]['dist_json'] if sys.argv[2] in d else '')" $ART $r)
    [ -n "$j" ] || continue
    cp -p $j "$D/dist-$r.json"
    grep -E 'acceptance guard|^go: ' $(dirname $j)/build.log > "$D/build-log-notes-$r.txt" 2>/dev/null || true
  done
  for x in start end rc out err; do cp -p $R/logs/build-$tag.$x "$D/build.$x.txt"; done
  G="git -c safe.directory=* -C $CL"
  refs=$(for r in baseline good defective startcheck realstart; do $G rev-parse -q --verify refs/upd1/$r >/dev/null && echo refs/upd1/$r; done)
  $G log --format='%H tree=%T parents=%P %s' $refs --no-walk > "$D/fixture-commits.txt"
  if [ $tag = cur ]; then
    { echo "=== efcba145 .. baseline (release policy label)"; $G diff efcba145543155b9c4104f9d42538a685cfab17d refs/upd1/baseline;
      echo "=== baseline .. good (release policy label)"; $G diff refs/upd1/baseline refs/upd1/good;
      echo "=== good .. defective"; $G diff refs/upd1/good refs/upd1/defective;
      echo "=== good .. startcheck"; $G diff refs/upd1/good refs/upd1/startcheck;
      echo "=== good .. realstart"; $G diff refs/upd1/good refs/upd1/realstart; } > "$D/fixture-patches.diff"
  else
    TAG=bd14d97efc5cfd19acd70ddf0edb9c6343317e2b
    { echo "=== published tag v0.1.0-alpha.80 ($TAG) .. baseline fixture (the whole seam patch)"; $G diff $TAG refs/upd1/baseline;
      echo "=== efcba145 .. good (release policy label)"; $G diff efcba145543155b9c4104f9d42538a685cfab17d refs/upd1/good;
      echo "=== good .. defective"; $G diff refs/upd1/good refs/upd1/defective; } > "$D/fixture-patches.diff"
  fi
done
cp -p $R/build/* "$S/build/"
for f in $R/logs/offline-*.txt; do cp -p $f "$S/build/"; done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-efcba145-files.sha256 "$S/harness-run-copy/"
for o in $R/overlay*; do mkdir -p "$S/harness-run-copy/$(basename $o)"; cp -p $o/* "$S/harness-run-copy/$(basename $o)/"; done
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
cp -p $P/setup-runcopy.sh $P/overlay.sh $P/bg.sh $P/job-build-cur.sh $P/job-build-a80.sh $P/job-offline.sh $P/prove.sh $P/mkcell.sh $P/startcell.sh "$S/harness-run-copy/"
for f in hostcheck.sh jobstat.sh waitjob.sh waitcell.sh peek.sh pkview.py pkv.sh stepview.py sv.sh showfail.sh probe-a.sh ext9.py scan9.py scan.sh stage9.sh sums.sh leftovers.sh fixcur.sh pkcross.py attempts.sh setupsum.py ssum.sh termsum.py tsum.sh pkj.sh cjrn.sh; do [ -f $P/$f ] && cp -p $P/$f "$S/tools/"; done
for f in hostcheck-before.txt hostcheck-during-cell-b.txt c-drive.txt image-check.txt host-leftovers.txt packagekit-crosstab.txt; do [ -f $P/$f ] && cp -p $P/$f "$S/host/"; done
for x in start end rc out err; do [ -f $R/logs/probe-a.$x ] && cp -p $R/logs/probe-a.$x "$S/host/packagekit-probe-a.$x.txt"; done
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
  python3 $P/ext9.py $lab > "$D/side/extract.txt" 2>&1
done
find "$S" -type f | wc -l; du -sh "$S"
