#!/bin/bash
# set2: stage what is common to the run (build, run copies, host records, tools) into the evidence folder.
set -uo pipefail
R=/var/tmp/cp-set2-run; L=$R/logs
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set2-20261011'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
B=$(dirname $(cat $R/art-cur.txt))
mkdir -p "$E/build/cur" "$E/harness-run-copy/jobs" "$E/host" "$E/tools"
# build
cp $B/upd1-artifacts.json "$E/build/cur/"
for r in baseline good defective startcheck realstart; do
  d=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]]['dist_json'])" $B/upd1-artifacts.json $r 2>/dev/null) && [ -f "$d" ] && cp "$d" "$E/build/cur/dist-$r.json"
done
git -c safe.directory='*' -C $B/repo log --format='%H tree=%T parents=%P %s' faa5ef085..HEAD --all 2>/dev/null | sort > "$E/build/cur/fixture-commits.txt"
base=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['baseline']['commit'])" $B/upd1-artifacts.json)
good=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['good']['commit'])" $B/upd1-artifacts.json)
{ echo "### baseline fixture commit $base against the source faa5ef085"; git -c safe.directory='*' -C $B/repo diff --stat faa5ef085571fa5183f436fa7150844041f873eb $base; git -c safe.directory='*' -C $B/repo diff faa5ef085571fa5183f436fa7150844041f873eb $base;
  echo "### good fixture commit $good against the baseline"; git -c safe.directory='*' -C $B/repo diff $base $good; } > "$E/build/cur/fixture-patches.diff" 2>&1
echo "source tree $(git -c safe.directory='*' -C "$REPO" rev-parse 'faa5ef085^{tree}'); baseline tree $(git -c safe.directory='*' -C $B/repo rev-parse "$base^{tree}")" > "$E/build/cur/baseline-tree-equals-source.txt"
git -c safe.directory='*' -C "$REPO" rev-parse 'faa5ef085^{commit}' > "$E/build/source-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$E/build/go-version.txt"
for x in start end rc out err; do cp $L/build.$x "$E/build/cur/build.$x.txt"; cp $L/prove.$x "$E/build/prove.$x.txt" 2>/dev/null; done
cp $R/build/cur-prove.json $R/build/cur-prove.stderr.txt "$E/build/" 2>/dev/null
cp $R/build/set2-dry-*.json "$E/build/" 2>/dev/null
for f in $L/offline-*; do cp "$f" "$E/build/$(basename $f | sed 's/\.\(start\|end\|rc\|out\|err\)$/.\1.txt/')"; done
# run copies
cp $R/runcopy-faa5ef085-files.sha256 "$E/harness-run-copy/"
for o in $R/overlay-*; do n=$(basename $o); mkdir -p "$E/harness-run-copy/$n"; cp $o/files.sha256 $o/harness.diff $o/differs-from-archive.txt "$E/harness-run-copy/$n/"; done
cp $J/job-cell-*.sh $J/job-cell-*.harness "$E/harness-run-copy/jobs/" 2>/dev/null
# host
cp $J/c-drive.txt $J/keepawake.log $J/hostcheck-before.txt "$E/host/" 2>/dev/null
# tools (the scripts of this run; no secret)
for f in $J/*.sh $J/*.py $J/*.ps1 $J/files.txt; do case $(basename $f) in orig-*) ;; *) cp "$f" "$E/tools/";; esac; done
find "$E" -type f | wc -l
