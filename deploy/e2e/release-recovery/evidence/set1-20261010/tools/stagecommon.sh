#!/bin/bash
# set1: stage the build, proof, dry runs, offline logs, run-copy records, jobs, scratch tools and host records into
# the repository evidence folder. Host only; read-only on the clone, labs and run copies. Run once.
set -u
R=/var/tmp/cp-set1-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set1
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set1-20261010"
SRC=c4cf7fd9d49677b16a60bd05bbe90f08be73cde6
[ -e "$S/build" ] && { echo "refusing: $S/build exists"; exit 2; }
mkdir -p "$S/build/cur" "$S/harness-run-copy/jobs" "$S/tools" "$S/host"
B=$(dirname $(cat $R/art-cur.txt)); ART=$B/upd1-artifacts.json; CL=$B/repo; D="$S/build/cur"
cp -p $ART "$D/"
for r in baseline good defective startcheck realstart; do
  j=$(python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d[sys.argv[2]]['dist_json'] if sys.argv[2] in d else '')" $ART $r)
  [ -n "$j" ] || continue
  cp -p $j "$D/dist-$r.json"
  grep -E 'acceptance guard|^go: ' $(dirname $j)/build.log > "$D/build-log-notes-$r.txt" 2>/dev/null || true
done
for x in start end rc out err; do cp -p $R/logs/build-h23.$x "$D/build.$x.txt"; cp -p $R/logs/build-cur.$x "$D/build-first-attempt.$x.txt"; done
G="git -c safe.directory=* -C $CL"
refs=$($G for-each-ref --format='%(refname)' refs/upd1/)
$G log --format='%H tree=%T parents=%P %s' $refs --no-walk > "$D/fixture-commits.txt"
{ echo "=== $SRC .. baseline (empty: the source already carries the v0.1.0-alpha.81 / 81 label)"; $G diff $SRC refs/upd1/baseline;
  echo "=== baseline .. good"; $G diff refs/upd1/baseline refs/upd1/good;
  for r in $refs; do case $r in */baseline|*/good) ;; *) echo "=== good .. ${r#refs/upd1/}"; $G diff refs/upd1/good $r;; esac; done; } > "$D/fixture-patches.diff"
echo "source tree $(git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse $SRC^{tree}); baseline tree $($G rev-parse refs/upd1/baseline^{tree})" > "$D/baseline-tree-equals-source.txt"
cp -p $R/build/* "$S/build/" 2>/dev/null
for f in $R/logs/offline-*.txt /tmp/set1-dry-*.json; do [ -f "$f" ] && cp -p $f "$S/build/"; done
for n in offline-p offline-set1 offline-h24 prove; do
  for x in start end rc out err; do [ -f $R/logs/$n.$x ] && cp -p $R/logs/$n.$x "$S/build/$n.$x.txt"; done
done
cp -p $R/harness-commit.txt "$S/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$S/build/go-version.txt"
cp -p $R/runcopy-c4cf7fd9-files.sha256 "$S/harness-run-copy/"
for o in $R/overlay-*; do n=$(basename $o); mkdir -p "$S/harness-run-copy/$n"; cp -p $o/files.sha256 $o/harness.diff $o/differs-from-archive.txt "$S/harness-run-copy/$n/"; done
cp -p $R/jobs/* "$S/harness-run-copy/jobs/"
for f in $P/*.sh $P/*.py $P/*.ps1 $P/files.txt; do [ -f "$f" ] && cp -p "$f" "$S/tools/"; done
for f in hostcheck-before.txt c-drive.txt removals.txt host-leftovers.txt keepawake.log; do [ -f $P/$f ] && cp -p $P/$f "$S/host/"; done
find "$S" -type f | wc -l; du -sh "$S" | cut -f1
