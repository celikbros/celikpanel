#!/bin/bash
# set4b: stage what is common to the run (the candidate source and its build, run copies, host records, tools) into
# the evidence folder. Re-runnable: it only copies.
set -uo pipefail
R=/var/tmp/cp-set4b-run; L=$R/logs
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set4b-20261009'
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
G() { git -c safe.directory='*' "$@"; }
mkdir -p "$E/build" "$E/harness-run-copy/jobs" "$E/host" "$E/tools" "$E/verification"
# the candidate source (a commit that exists only in the disposable clone) and its build
cp $R/src-c1.txt "$E/build/candidate-source.txt"
cp $R/src-c1.diff "$E/build/candidate-source.diff"
cp $R/src-c1-files.sha256 "$E/build/candidate-source-files.sha256"
cp $R/src-c1-listed-vs-committed.txt "$E/build/candidate-source-listed-vs-committed.txt"
ART=$(cat $R/artifacts-c1.path)
B=$(dirname $ART)
cp $ART "$E/build/upd1-artifacts.json"
field() { python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],{}).get(sys.argv[3],''))" "$1" "$2" "$3"; }
for r in baseline good defective startcheck realstart; do
  d=$(field $ART $r dist_json); [ -n "$d" ] && [ -f "$d" ] && cp "$d" "$E/build/dist-$r.json"
done
G -C $B/repo for-each-ref --format='%(refname) %(objectname) tree=%(tree) %(subject)' refs/upd1 > "$E/build/fixture-commits.txt"
base=$(field $ART baseline commit); cand=$(sed -n 's/^candidate=//p' $R/src-c1.txt)
{
  echo "candidate $cand tree $(G -C $B/repo rev-parse "$cand^{tree}"); baseline $base tree $(G -C $B/repo rev-parse "$base^{tree}")"
  echo "### git diff --stat: the candidate source against this build's baseline (the installed archive)"
  G -C $B/repo diff --stat $cand $base | tail -n 1
  [ -z "$(G -C $B/repo diff --stat $cand $base)" ] && echo "(no difference: the baseline fixture commit is an empty commit over the candidate)"
} > "$E/build/trees.txt" 2>&1
cp $L/build-c1.out "$E/build/build.out.txt"; cp $L/build-c1.err "$E/build/build.err.txt"
for x in start end rc out err; do [ -f $L/build.$x ] && cp $L/build.$x "$E/build/build-job.$x.txt"; done
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$E/build/go-version.txt"
cp $R/build/*.json $R/build/*.txt "$E/build/" 2>/dev/null
for f in $L/offline-*; do cp "$f" "$E/build/$(basename $f)"; done
for f in $L/py-*.txt; do [ -f "$f" ] && cp "$f" "$E/verification/$(basename $f)"; done
for f in $L/mkov-*.txt; do cp "$f" "$E/harness-run-copy/$(basename $f)"; done
cp $R/runcopy-e2be8af30-files.sha256 "$E/harness-run-copy/"
for o in $R/overlay-*; do n=$(basename $o); mkdir -p "$E/harness-run-copy/$n"; cp $o/files.sha256 $o/harness.diff $o/differs-from-archive.txt "$E/harness-run-copy/$n/"; done
cp $R/job-*.sh "$E/harness-run-copy/jobs/" 2>/dev/null
for f in $L/stage-*.txt; do [ -f "$f" ] && cp "$f" "$E/harness-run-copy/jobs/$(basename $f)"; done
cp $R/progress.txt "$E/host/progress.txt"
for f in c-drive.txt keepawake.log hostcheck-before.txt sleep-events.txt; do [ -f "$J/$f" ] && cp "$J/$f" "$E/host/"; done
for f in $J/*.sh $J/*.py $J/*.ps1 $J/files.txt $J/candidate-files.txt $J/*.en.txt $J/*.tr.txt $J/contract.tr.?.txt $J/REMEASURE.txt; do
  [ -f "$f" ] && sed "s#/mnt/c/Users/[^/]*/AppData/Local/Temp/claude/[^/]*/[^/]*/scratchpad#<scratchpad>#g; s#C:\\\\Users\\\\[^\\\\]*\\\\AppData\\\\Local\\\\Temp\\\\claude\\\\[^\\\\]*\\\\[^\\\\]*\\\\scratchpad#<scratchpad>#g" "$f" > "$E/tools/$(basename $f)"
done
# the verification on the final working tree (logs written beside this script; the two large `go test -json` logs
# are not kept, their failing sets and counts are)
for f in go-final-id.txt go-final-gofmt.txt go-final-vet.txt go-final-build-amd64.txt go-final-build-arm64.txt          go-baseline-id.txt go-baseline-failset.txt go-final-agent-failset.txt go-final-other-failset.txt go-final-summary.txt          product-files-after-the-build.txt worktree-files.txt; do
  [ -f "$J/$f" ] && sed 's/$//' "$J/$f" > "$E/verification/$f"
done
for f in web-build-final.log web-test-final.log; do [ -f "$J/$f" ] && sed 's/$//' "$J/$f" > "$E/verification/$f.txt"; done
find "$E" -type f | wc -l
