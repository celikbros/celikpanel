#!/bin/bash
# upd7: stage build, run-copy, tool records and the cells into the repository evidence folder.
# Host only; read-only on the labs, the clone and the run copy. Refuses an existing folder.
# usage: stage7.sh SHORT:LAB:NODE:CELL:RUN ...
set -u
R=/var/tmp/cp-upd7-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd7
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd7-20261001"
[ -e "$S" ] && { echo "refusing: $S exists"; exit 2; }
mkdir -p "$S/build" "$S/harness-run-copy/jobs" "$S/harness-run-copy/overlay" "$S/tools" "$S/host"
ART=$(cat $R/ART); B=$(dirname $ART); CL=$B/repo
cp -p $ART "$S/build/"
cp -p $B/baseline-ref-proof.txt $B/agent-deps.txt "$S/build/"
for r in baseline good defective; do
  j=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]]['dist_json'])" $ART $r)
  cp -p $j "$S/build/dist-$r.json"
  grep -E 'acceptance guard|^go: ' $(dirname $j)/build.log > "$S/build/build-log-notes-$r.txt" 2>/dev/null || true
done
cp -p $R/build/* "$S/build/"
for x in start end rc out err; do cp -p $R/logs/build.$x "$S/build/build.$x.txt"; done
for t in test_owner_update_trial test_recovery_candidate_archive test_current_worker_baseline test_worker_fixture_origin test_bound_worker_reboot test_guest_bound_worker; do
  cp -p $R/logs/offline-$t.txt "$S/build/offline-$t.txt"
done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
G="git -c safe.directory=* -C $CL"
TAG=bd14d97efc5cfd19acd70ddf0edb9c6343317e2b
$G log --format='%H tree=%T parents=%P %s' refs/upd1/baseline refs/upd1/good refs/upd1/defective --no-walk > "$S/build/fixture-commits.txt"
{ echo "=== published tag v0.1.0-alpha.80 ($TAG) .. baseline fixture (the whole seam patch)"; $G diff $TAG refs/upd1/baseline;
  echo "=== 48d21d58 .. good (release policy label)"; $G diff 48d21d58605fe6b6a56d656dba566a2afc12f6b4 refs/upd1/good;
  echo "=== good .. defective"; $G diff refs/upd1/good refs/upd1/defective; } > "$S/build/fixture-patches.diff"
$G show 48d21d58605fe6b6a56d656dba566a2afc12f6b4:internal/licensing/acceptance_fixture.go | diff -u --label head/acceptance_fixture.go --label alpha80-fixture/acceptance_fixture.go - <($G show refs/upd1/baseline:internal/licensing/acceptance_fixture.go) > "$S/build/seam-adaptation-vs-source.diff"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-48d21d58-files.sha256 "$S/harness-run-copy/"
cp -p $R/overlay/* "$S/harness-run-copy/overlay/"
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
cp -p $P/setup-runcopy.sh $P/overlay.sh $P/bg.sh $P/job-build.sh $P/job-offline.sh $P/setart.sh $P/mkcell.sh $P/startcell.sh "$S/harness-run-copy/"
cp -p $P/hostcheck.sh $P/jobstat.sh $P/waitjob.sh $P/waitcell.sh $P/peek.sh $P/grepev.sh $P/ext7.py $P/scan7.py $P/scan.sh $P/stage7.sh "$S/tools/"
cp -p $P/hostcheck-before.txt $P/c-drive.txt "$S/host/"
[ -f $P/host-leftovers.txt ] && cp -p $P/host-leftovers.txt "$S/host/"
for spec in "$@"; do
  IFS=: read -r short lab node cell run <<< "$spec"
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
  python3 $P/ext7.py $lab > "$D/side/extract.txt" 2>&1
done
find "$S" -type f | wc -l; du -sh "$S"
