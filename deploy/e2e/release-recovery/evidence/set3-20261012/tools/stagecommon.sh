#!/bin/bash
# set3: stage what is common to the run (builds, proofs, run copies, host records, tools) into the evidence folder.
set -uo pipefail
R=/var/tmp/cp-set3-run; L=$R/logs
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set3-20261012'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
G() { git -c safe.directory='*' "$@"; }
SRC=$(G -C "$REPO" rev-parse 'cfa329676^{commit}')
TAG=$(G -C "$REPO" rev-parse 'v0.1.0-alpha.81^{commit}')
mkdir -p "$E/build" "$E/harness-run-copy/jobs" "$E/host" "$E/tools"
field() { python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],{}).get(sys.argv[3],''))" "$1" "$2" "$3"; }
stage_build() {  # name build-dir log-name
  local n=$1 B=$2 log=$3 base good c d
  mkdir -p "$E/build/$n"
  cp $B/upd1-artifacts.json "$E/build/$n/"
  [ -f $B/baseline-ref-proof.txt ] && cp $B/baseline-ref-proof.txt "$E/build/$n/"
  [ -f $B/agent-deps.txt ] && cp $B/agent-deps.txt "$E/build/$n/"
  for r in baseline good defective startcheck realstart; do
    d=$(field $B/upd1-artifacts.json $r dist_json)
    [ -n "$d" ] && [ -f "$d" ] && cp "$d" "$E/build/$n/dist-$r.json"
  done
  G -C $B/repo for-each-ref --format='%(refname) %(objectname) tree=%(tree) %(subject)' refs/upd1 > "$E/build/$n/fixture-commits.txt"
  base=$(field $B/upd1-artifacts.json baseline commit)
  good=$(field $B/upd1-artifacts.json good commit)
  {
    echo "### git diff --stat: the published tag v0.1.0-alpha.81 ($TAG) against this build's baseline $base"
    G -C $B/repo diff --stat $TAG $base | tail -n 3
    echo "### git diff --stat: the source cfa329676 ($SRC) against this build's baseline $base"
    G -C $B/repo diff --stat $SRC $base | tail -n 1
    echo "### git diff: the source cfa329676 ($SRC) against the good candidate $good"
    G -C $B/repo diff $SRC $good
    for r in defective startcheck realstart; do
      c=$(field $B/upd1-artifacts.json $r commit)
      if [ -n "$c" ]; then echo "### git diff: the good candidate against $r $c"; G -C $B/repo diff $good $c; fi
    done
  } > "$E/build/$n/fixture-patches.diff" 2>&1
  echo "source cfa329676 tree $(G -C "$REPO" rev-parse 'cfa329676^{tree}'); tag v0.1.0-alpha.81 tree $(G -C "$REPO" rev-parse 'v0.1.0-alpha.81^{tree}'); baseline $base tree $(G -C $B/repo rev-parse "$base^{tree}"); good $good tree $(G -C $B/repo rev-parse "$good^{tree}")" > "$E/build/$n/trees.txt"
  for x in start end rc out err; do [ -f $L/$log.$x ] && cp $L/$log.$x "$E/build/$n/build.$x.txt"; done
}
stage_build cur /var/tmp/cp-upd1-build/20261009t061555z build
stage_build a81-first /var/tmp/cp-upd1-build/20261009t064831z build81
stage_build a81 /var/tmp/cp-upd1-build/20261009t070418z build81b
echo "$SRC" > "$E/build/source-commit.txt"; echo "$TAG" > "$E/build/tag-v0.1.0-alpha.81-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$E/build/go-version.txt"
cp $R/build/*.json $R/build/*.txt "$E/build/" 2>/dev/null
for n in prove prove2; do for x in start end rc out err; do [ -f $L/$n.$x ] && cp $L/$n.$x "$E/build/$n.$x.txt"; done; done
for f in $L/offline-*; do cp "$f" "$E/build/$(basename $f)"; done
for f in $L/mkov-*.txt; do cp "$f" "$E/harness-run-copy/$(basename $f)"; done
cp $R/runcopy-cfa329676-files.sha256 "$E/harness-run-copy/"
for o in $R/overlay-*; do n=$(basename $o); mkdir -p "$E/harness-run-copy/$n"; cp $o/files.sha256 $o/harness.diff $o/differs-from-archive.txt "$E/harness-run-copy/$n/"; done
cp $J/job-*.sh $J/job-*.harness "$E/harness-run-copy/jobs/" 2>/dev/null
cp $L/queue.log "$E/harness-run-copy/queue.log" 2>/dev/null
for f in $L/queue-W*.out $L/stage-*.txt; do [ -f "$f" ] && cp "$f" "$E/harness-run-copy/jobs/$(basename $f)"; done
cp $J/c-drive.txt $J/c-drive-watch.txt $J/keepawake.log $J/hostcheck-before.txt "$E/host/" 2>/dev/null
cp $R/c-drive-cells.txt "$E/host/" 2>/dev/null
for f in $J/*.sh $J/*.py $J/*.ps1 $J/files.txt; do cp "$f" "$E/tools/"; done
find "$E" -type f | wc -l
