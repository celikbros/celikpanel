#!/bin/bash
# set6: stage what is common to the run (builds, proofs, run copies, host records, tools) into the evidence folder.
set -uo pipefail
R=/var/tmp/cp-set6-run; L=$R/logs
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
G() { git -c safe.directory='*' "$@"; }
SRC=$(G -C "$REPO" rev-parse '72b879eea^{commit}')
TAG=$(G -C "$REPO" rev-parse 'v0.1.0-alpha.81^{commit}')
mkdir -p "$E/build/cur" "$E/build/a81" "$E/harness-run-copy/jobs" "$E/host" "$E/tools"
field() { python3 -I -c "import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],{}).get(sys.argv[3],''))" "$1" "$2" "$3"; }
for b in cur a81; do
  ART=$(cat $R/artifacts-$b.path); B=$(dirname "$ART")
  cp "$ART" "$E/build/$b/"
  cp $B/baseline-ref-proof.txt $B/agent-deps.txt "$E/build/$b/" 2>/dev/null
  for r in baseline good defective startcheck realstart; do
    d=$(field "$ART" $r dist_json)
    [ -n "$d" ] && [ -f "$d" ] && cp "$d" "$E/build/$b/dist-$r.json"
    [ -n "$d" ] && [ -f "$(dirname "$d")/build.log" ] && cp "$(dirname "$d")/build.log" "$E/build/$b/dist-$r.build.log.txt"
  done
  G -C $B/repo for-each-ref --format='%(refname) %(objectname) tree=%(tree) %(subject)' refs/upd1 > "$E/build/$b/fixture-commits.txt"
  base=$(field "$ART" baseline commit); good=$(field "$ART" good commit)
  {
    if [ $b = a81 ]; then
      echo "### git diff --stat: the published tag v0.1.0-alpha.81 ($TAG) against this build's baseline $base"
      G -C $B/repo diff --stat $TAG $base | tail -n 3
      echo "(end of that diff; empty means the baseline is the tag's own commit)"
    else
      echo "### git diff: the source 72b879eea ($SRC) against this build's baseline $base (the archive the fresh-install cells install)"
      G -C $B/repo diff $SRC $base
      echo "(end of that diff; empty means the baseline's tree is the source's tree)"
    fi
    echo "### git diff: the source 72b879eea ($SRC) against the good candidate $good"
    G -C $B/repo diff $SRC $good
    for r in defective startcheck realstart; do
      c=$(field "$ART" $r commit)
      if [ -n "$c" ]; then echo "### git diff: the good candidate against $r $c"; G -C $B/repo diff $good $c; fi
    done
  } > "$E/build/$b/fixture-patches.diff" 2>&1
  {
    echo "source 72b879eea commit $SRC tree $(G -C "$REPO" rev-parse '72b879eea^{tree}')"
    echo "tag v0.1.0-alpha.81 commit $TAG tree $(G -C "$REPO" rev-parse 'v0.1.0-alpha.81^{tree}')"
    echo "baseline $base tree $(G -C $B/repo rev-parse "$base^{tree}")"
    echo "good $good tree $(G -C $B/repo rev-parse "$good^{tree}")"
    echo "files that differ between the source 72b879eea and this build's baseline: $(G -C $B/repo diff --name-only $SRC $base | wc -l)"
    echo "files that differ between the source 72b879eea and the good candidate: $(G -C $B/repo diff --name-only $SRC $good | tr '\n' ' ')"
    echo "product files (cmd internal web) that differ between 72b879eea and the branch head $(G -C "$REPO" rev-parse HEAD): $(G -C "$REPO" diff --name-only $SRC HEAD -- cmd internal web | wc -l)"
    echo "product commits between 67b62cc0f (set5's candidate) and 72b879eea (git log --oneline 67b62cc0f..72b879eea -- cmd internal web):"
    G -C "$REPO" log --oneline 67b62cc0f..72b879eea -- cmd internal web
    echo "files of cmd internal web that differ between 67b62cc0f and 72b879eea:"
    G -C "$REPO" diff --name-only 67b62cc0f 72b879eea -- cmd internal web
  } > "$E/build/$b/trees.txt"
  cp $L/build-$b.out "$E/build/$b/build.out.txt"; cp $L/build-$b.err "$E/build/$b/build.err.txt"
done
for x in start end rc out err; do [ -f $L/build.$x ] && cp $L/build.$x "$E/build/build-job.$x.txt"; done
echo "$SRC" > "$E/build/source-commit.txt"; echo "$TAG" > "$E/build/tag-v0.1.0-alpha.81-commit.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$E/build/go-version.txt"
cp $R/build/*.json $R/build/*.txt "$E/build/" 2>/dev/null
for f in $L/offline-* $L/suite-* $L/afterbuild-*.txt; do [ -f "$f" ] && cp "$f" "$E/build/$(basename $f)"; done
for f in $L/mkov-*.txt; do cp "$f" "$E/harness-run-copy/$(basename $f)"; done
for o in $R/overlay-*; do
  n=$(basename $o); mkdir -p "$E/harness-run-copy/$n"
  cp $o/files.sha256 $o/harness.diff $o/differs-from-archive.txt $o/runcopy-against-commit.txt "$E/harness-run-copy/$n/" 2>/dev/null
done
# the pristine file list is the same for every copy (the archive of the commit without earlier evidence): kept once
cp $R/overlay-a/pristine-files.sha256 "$E/harness-run-copy/pristine-files.sha256" 2>/dev/null
for o in $R/overlay-*; do cmp -s $R/overlay-a/pristine-files.sha256 $o/pristine-files.sha256 && echo "$(basename $o): pristine file list equal to overlay-a's" || echo "$(basename $o): pristine file list DIFFERS from overlay-a's"; done > "$E/harness-run-copy/pristine-files-compared.txt"
cp $R/job-*.sh "$E/harness-run-copy/jobs/" 2>/dev/null
cp $L/queue.log "$E/harness-run-copy/queue.log" 2>/dev/null
for f in $L/queue-*.out $L/stage-*.txt $L/stage-*.json; do [ -f "$f" ] && cp "$f" "$E/harness-run-copy/jobs/$(basename $f)"; done
cp $R/progress.txt "$E/host/progress.txt"
cp $J/c-drive.txt $J/c-drive-watch.txt $J/keepawake.log $J/hostcheck-before.txt $J/sleep-events.txt $J/working-tree-status.txt "$E/host/" 2>/dev/null
cp $R/c-drive-cells.txt $R/working-tree-against-copy-*.txt "$E/host/" 2>/dev/null
[ -f $R/removals-build.txt ] && cp $R/removals-build.txt "$E/host/removals-build.txt"
for f in $J/*.sh $J/*.py $J/*.ps1 $J/files.txt; do cp "$f" "$E/tools/"; done
[ -f $J/README.source.md ] && cp $J/README.source.md "$E/tools/README.source.md"
# the local scratch-folder prefix is not part of the record
sed -i 's#/mnt/c/Users/[^/]*/AppData/Local/Temp/claude/[^/]*/[^/]*/scratchpad#<scratchpad>#g' "$E"/tools/*.sh "$E"/harness-run-copy/jobs/*.sh "$E"/harness-run-copy/jobs/*.out "$E"/harness-run-copy/jobs/*.txt 2>/dev/null
find "$E" -type f | wc -l
