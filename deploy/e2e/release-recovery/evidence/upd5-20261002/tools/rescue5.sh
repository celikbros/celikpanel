#!/bin/bash
# Rescue the upd5 evidence from the stopped labs into the repo folder (read-only on labs and the run copy).
set -u
R=/var/tmp/cp-upd5-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd5run
Q=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/disk
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd5-20261002"
[ -e "$S" ] && { echo "refusing: $S exists"; exit 2; }
mkdir -p "$S"
CP="cp -r --preserve=timestamps"
for spec in d13good:d13-good-a:debian13:upd1-debian13-good d13rs:d13-rs-a:debian13:upd1-debian13-realstart d13oc:d13-oc-a:debian13:upd1-debian13-owner-continuation archgood:arch-good-a:arch:upd1-arch-good archrs:arch-rs-a:arch:upd1-arch-realstart archoc:arch-oc-a:arch:upd1-arch-owner-continuation; do
  IFS=: read short lab node cell <<< "$spec"
  L=/var/tmp/cp-release-drill-upd5-$lab
  D="$S/$cell/run-a"
  mkdir -p "$D/host" "$D/side"
  ev=$(ls -d $L/evidence/$node/upd1/*/ | tail -1)
  $CP "$ev"/. "$D/" || echo "COPY ERROR $cell driver evidence"
  ( cd "$D" && sha256sum -c --quiet SHA256SUMS > /dev/null 2>&1 && echo "$cell driver SHA256SUMS ok ($(wc -l < SHA256SUMS) entries)" || echo "$cell driver SHA256SUMS FAILED" )
  for x in start end rc out err; do [ -f $R/logs/cell-$short.$x ] && cp --preserve=timestamps $R/logs/cell-$short.$x "$D/host/wrapper.$x.txt"; done
  cp --preserve=timestamps $R/jobs/job-cell-$short.sh "$D/host/job.sh"
  cp --preserve=timestamps $R/jobs/job-cell-$short.harness "$D/host/harness.txt"
  { echo "lab=$L"; basename $ev; } > "$D/host/lab.txt"
  for f in $L/evidence/$node/*.json $L/evidence/$node/*.jsonl; do [ -f "$f" ] && cp --preserve=timestamps "$f" "$D/host/"; done
  for f in $L/lab.json $L/current-worker-baseline-$node-current-worker-baseline-result.json $L/current-worker-baseline-$node-current-worker-baseline.log $L/worker-origin/worker-origin-intent.json $L/worker-origin/worker-origin-manifest $L/cells/*/fixture-plan.json; do
    if [ -f "$f" ]; then cp --preserve=timestamps "$f" "$D/host/"; else echo "  $cell: absent $f"; fi
  done
  python3 $P/ext5.py upd5-$lab $node "$D/side/extract.txt"
  if cmp -s "$D/side/extract.txt" $P/pulled/$cell-run-a-extract.txt; then echo "  extract equals pulled copy"; else echo "  extract differs from pulled copy ($(wc -c < "$D/side/extract.txt") vs $(wc -c < $P/pulled/$cell-run-a-extract.txt) bytes)"; fi
  echo "  zero-size files in lab (excluding images): $(find $L -path $L/images -prune -o -type f -size 0 -print | wc -l)"
  echo "  unreadable in lab: $(find $L -path $L/images -prune -o -type f ! -readable -print 2>&1 | wc -l)"
done
# run-b of arch owner continuation: lab created, nothing measured
LB=/var/tmp/cp-release-drill-upd5-arch-oc-b
echo "== arch-oc-b"; ls -la $LB; find $LB -path $LB/images -prune -o -type f -print | head
# build records (as stage-common.sh)
B="$S/build"; mkdir -p "$B"
ART=$(cat $R/ART); CL=$(dirname $ART)/repo
cp --preserve=timestamps $ART "$B/"
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]]['dist_json'])" $ART $r)
  cp --preserve=timestamps $j "$B/dist-$r.json" || echo "missing dist $r"
done
cp --preserve=timestamps $R/build/* "$B/"
for x in start end rc out err; do cp --preserve=timestamps $R/logs/build.$x "$B/build.$x.txt"; cp --preserve=timestamps $R/logs/offline.$x "$B/offline-job.$x.txt"; done
cp --preserve=timestamps $R/logs/offline-owner.txt "$B/offline-tests-owner-6cda60b8.txt"
cp --preserve=timestamps $R/logs/offline-archive.txt "$B/offline-tests-archive-6cda60b8.txt"
cp --preserve=timestamps $R/harness-commit.txt "$B/source-commit.txt"
git -c safe.directory='*' -C $CL log --format='%H tree=%T parents=%P %s' refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start --no-walk > "$B/fixture-commits.txt"
for c in $(git -c safe.directory='*' -C $CL rev-parse refs/upd1/baseline refs/upd1/good refs/upd1/defective refs/upd1/start-check refs/upd1/real-start); do
  echo "=== 6cda60b8..$c"; git -c safe.directory='*' -C $CL diff --stat 6cda60b87e8144ae33aaa343d04a9c3923d8755e $c
done > "$B/fixture-commits-stat.txt"
{ echo "=== start-check over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/start-check; echo "=== real-start over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/real-start; echo "=== defective over good"; git -c safe.directory='*' -C $CL diff refs/upd1/good refs/upd1/defective; } > "$B/fixture-kind-patches.diff"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$B/go-version.txt"
# harness run copy records
H="$S/harness-run-copy"; mkdir -p "$H/jobs"
cp --preserve=timestamps $R/runcopy-files.sha256 "$H/runcopy-6cda60b8-files.sha256"
for f in $R/jobs/*; do if [ -s "$f" ]; then cp --preserve=timestamps "$f" "$H/jobs/"; else echo "  empty job file not copied: $f"; fi; done
( cd $R && diff -u harness/deploy/e2e/release-recovery/owner_update_trial.py harness-h16/deploy/e2e/release-recovery/owner_update_trial.py ) > "$H/H16-owner_update_trial.py.diff"
echo "H16 diff regenerated: $(wc -l < "$H/H16-owner_update_trial.py.diff") lines, hunks: $(grep -c '^@@' "$H/H16-owner_update_trial.py.diff")"
cp --preserve=timestamps $P/mk-h16.py $P/do-h16.sh $P/setup-runcopy.sh $P/bg.sh $P/job-build.sh $P/job-offline.sh $P/mkjobs.sh $P/setart.sh $P/prove-dry.sh $P/startcell.sh $P/stop-ocb.sh "$H/"
# tools and host records
T="$S/tools"; mkdir -p "$T"
cp --preserve=timestamps $P/hostcheck.sh $P/snapshot-host.sh $P/jobstat.sh $P/waitjob.sh $P/peek.sh $P/stagecell.sh $P/stage-common.sh $P/ext5.py $P/summary5.py $P/scan5.py $P/views.py $P/live.py $P/failpeek.sh $P/failpeek2.sh $P/reext.sh $P/serialpeek.sh $P/procs.sh "$T/"
cp $Q/rescue5.sh "$T/"
HO="$S/host"; mkdir -p "$HO"
cp --preserve=timestamps $P/hostcheck-before.txt $P/host-df-before.txt "$HO/"
echo "== done"; find "$S" -type f | wc -l; du -sh "$S"
