#!/bin/bash
# upd12: stage the build, proofs, dry runs, offline logs, the run copy record, jobs, scratch tools and host records
# into the repository evidence folder. Host only; read-only on the clone, labs and run copy. Refuses a second run.
set -u
R=/var/tmp/cp-upd12-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd12
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd12-20261002"
SRC=6b6f8a0c937e599c6b3b5c5f0c67cbef25c9bc81
[ -e "$S/build" ] && { echo "refusing: $S/build exists"; exit 2; }
mkdir -p "$S/build/cur" "$S/harness-run-copy/jobs" "$S/harness-run-copy/overlay" "$S/tools" "$S/host"
B=/var/tmp/cp-upd1-build/20261002t140055z; ART=$B/upd1-artifacts.json; CL=$B/repo; D="$S/build/cur"
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
{ echo "=== $SRC .. baseline"; $G diff $SRC refs/upd1/baseline;
  echo "=== baseline .. good"; $G diff refs/upd1/baseline refs/upd1/good;
  for r in $refs; do case $r in */baseline|*/good) ;; *) echo "=== good .. ${r#refs/upd1/}"; $G diff refs/upd1/good $r;; esac; done; } > "$D/fixture-patches.diff"
cp -p $R/build/* "$S/build/"
for f in $R/logs/offline-*.txt; do cp -p $f "$S/build/"; done
for n in offline-p offline-o1 prove-cur; do
  for x in start end rc out err; do [ -f $R/logs/$n.$x ] && cp -p $R/logs/$n.$x "$S/build/$n.$x.txt"; done
done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-6b6f8a0c-files.sha256 "$S/harness-run-copy/"
cp -p $R/overlay/* "$S/harness-run-copy/overlay/"
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
for f in setup-runcopy.sh overlay.sh bg.sh job-build-cur.sh go-build-cur.sh job-offline.sh job-offline-p.sh \
         job-offline-o1.sh go-offline-p.sh go-offline-o1.sh prove.sh job-prove-cur.sh go-prove.sh mkcell.sh go-cell.sh \
         startcell.sh wt-test.sh facts-try.sh; do
  [ -f $P/$f ] && cp -p $P/$f "$S/harness-run-copy/"
done
for f in hostcheck.sh jobstat.sh waitjob.sh waitcell.sh peek.sh cpaths.py ext12.py dml12.py dml.sh summary12.py \
         texts12.py labdu.sh stagecell12.sh stage-batch.sh rmoverlay12.sh stagecommon12.sh cdrive.ps1 keepawake.ps1 \
         scan.sh scan12.py sums.sh leftovers12.sh; do
  [ -f $P/$f ] && cp -p $P/$f "$S/tools/"
done
for f in hostcheck-before.txt c-drive.txt removals.txt host-leftovers.txt keepawake.log; do [ -f $P/$f ] && cp -p $P/$f "$S/host/"; done
find "$S" -type f | wc -l; du -sh "$S"
