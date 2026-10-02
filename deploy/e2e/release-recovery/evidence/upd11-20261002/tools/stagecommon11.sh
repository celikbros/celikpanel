#!/bin/bash
# upd11: stage the two builds, proofs, dry runs, offline logs, run copies (base, h20, h21), jobs, scratch tools and
# host records into the repository evidence folder. Host only; read-only on the clones, labs and run copies.
# Refuses when build/ already exists (run once).
set -u
R=/var/tmp/cp-upd11-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd11
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd11-20261002"
SRC=48e5465712c3ef94f10fa8f91cc952289902ec2d
[ -e "$S/build" ] && { echo "refusing: $S/build exists"; exit 2; }
mkdir -p "$S/build" "$S/harness-run-copy/jobs" "$S/tools" "$S/host"
for spec in cur:20261001t221215z a80:20261001t221533z; do
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
  base=$SRC; [ $tag = a80 ] && base=$($G rev-parse 'v0.1.0-alpha.80^{commit}' 2>/dev/null || echo bd14d97efc5cfd19acd70ddf0edb9c6343317e2b)
  { echo "=== $base .. baseline"; $G diff $base refs/upd1/baseline;
    echo "=== baseline .. good"; $G diff refs/upd1/baseline refs/upd1/good;
    for r in $refs; do case $r in */baseline|*/good) ;; *) echo "=== good .. ${r#refs/upd1/}"; $G diff refs/upd1/good $r;; esac; done; } > "$D/fixture-patches.diff"
done
cp -p $R/build/* "$S/build/"
for f in $R/logs/offline-*.txt; do cp -p $f "$S/build/"; done
for n in offline-p offline-o1 offline-h20 offline-h21 prove-cur prove-a80; do
  for x in start end rc out err; do [ -f $R/logs/$n.$x ] && cp -p $R/logs/$n.$x "$S/build/$n.$x.txt"; done
done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-48e54657-files.sha256 "$S/harness-run-copy/"
for o in overlay overlay-h20 overlay-h21; do mkdir -p "$S/harness-run-copy/$o"; cp -p $R/$o/* "$S/harness-run-copy/$o/"; done
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
for f in setup-runcopy.sh overlay.sh overlay-h20.sh overlay-h21.sh bg.sh job-build-cur.sh job-build-a80.sh go-build-cur.sh \
         go-build-a80.sh job-offline.sh job-offline-h20.sh job-offline-h21.sh job-offline-p.sh job-offline-o1.sh \
         job-offline-h20run.sh job-offline-h21run.sh go-offline-p.sh go-offline-o1.sh go-h20.sh go-h21.sh prove.sh \
         job-prove-cur.sh job-prove-a80.sh go-prove.sh dry-h20.sh mkcell.sh mkcell-h20.sh mkcell-h21.sh go-cell.sh \
         go-cell-h20.sh go-cell-h21.sh startcell.sh; do
  [ -f $P/$f ] && cp -p $P/$f "$S/harness-run-copy/"
done
for f in hostcheck.sh jobstat.sh waitjob.sh waitcell.sh peek.sh cpaths.py cpaths.sh listen.sh ext11.py summary11.py \
         texts11.py diag4.sh diag4b.sh jwin.sh diagmr.sh diagcron.sh diagcron2.sh diagtrack.sh diagagree.sh labdu.sh \
         stagecell11.sh stage-batch.sh rmoverlay11.sh stagecommon11.sh cdrive.ps1 scan.sh scan11.py sums.sh leftovers.sh \
         job-stage1.sh go-stage1.sh pkview.py pkv.sh; do
  [ -f $P/$f ] && cp -p $P/$f "$S/tools/"
done
for f in hostcheck-before.txt c-drive.txt removals.txt host-leftovers.txt; do [ -f $P/$f ] && cp -p $P/$f "$S/host/"; done
for x in start end rc out err; do [ -f $R/logs/stage1.$x ] && cp -p $R/logs/stage1.$x "$S/host/stage1.$x.txt"; done
find "$S" -type f | wc -l; du -sh "$S"
