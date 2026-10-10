#!/bin/bash
# set9: stage what is common to the run (build, proof, run copies, offline suites, host records).
set -uo pipefail
R=/var/tmp/cp-set9-run; L=$R/logs
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
G() { git -c safe.directory='*' "$@"; }
SRC=$(G -C "$REPO" rev-parse '7c3a05809^{commit}')
mkdir -p "$E/build/cur" "$E/harness-run-copy" "$E/host"
field() { python3 -I -c "import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],{}).get(sys.argv[3],''))" "$1" "$2" "$3"; }
ART=$(cat $R/artifacts-cur.path); B=$(dirname "$ART")
cp "$ART" "$E/build/cur/"
for r in baseline good; do
  d=$(field "$ART" $r dist_json)
  [ -n "$d" ] && [ -f "$d" ] && cp "$d" "$E/build/cur/dist-$r.json"
done
G -C $B/repo for-each-ref --format='%(refname) %(objectname) tree=%(tree) %(subject)' refs/upd1 > "$E/build/cur/fixture-commits.txt"
base=$(field "$ART" baseline commit); good=$(field "$ART" good commit)
{
  echo "source 7c3a05809 = $SRC tree $(G -C "$REPO" rev-parse "$SRC^{tree}") (e508af230 = $(G -C "$REPO" rev-parse 'e508af230^{commit}'))"
  echo "product code e508af230..7c3a05809 (cmd internal web/src): $(G -C "$REPO" diff --stat e508af230 7c3a05809 -- cmd internal web/src | wc -l) lines of diff --stat (0 = identical)"
  echo "baseline $base tree $(G -C $B/repo rev-parse "$base^{tree}")"
  echo "good     $good tree $(G -C $B/repo rev-parse "$good^{tree}")"
  echo "### files that differ between the source 7c3a05809 and the baseline $base"
  G -C $B/repo diff --name-status $SRC $base
  echo "### files that differ between the source 7c3a05809 and the good candidate $good"
  G -C $B/repo diff --name-status $SRC $good
  echo "### files of web/ that differ between the source and the baseline / the good candidate: $(G -C $B/repo diff --name-only $SRC $base -- web | wc -l) / $(G -C $B/repo diff --name-only $SRC $good -- web | wc -l)"
} > "$E/build/cur/trees.txt"
G -C $B/repo diff $SRC $base > "$E/build/cur/fixture-patch-baseline.diff"
G -C $B/repo diff $SRC $good > "$E/build/cur/fixture-patch-good.diff"
cp $R/build/cur-prove.json "$E/build/cur-prove.json"; cp $R/build/dry-cur.json "$E/build/dry-cur.json"; cp $R/build/dry-cur.rc.txt "$E/build/dry-cur.rc.txt"
tail -n 40 $L/build-cur.err > "$E/build/build-cur.err.tail.txt"; cp $L/build-cur.out "$E/build/build-cur.out.txt"
cp $L/build.out "$E/build/build-job.out.txt"; cp $L/build.start "$E/build/build-job.start.txt"; cp $L/build.end "$E/build/build-job.end.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$E/build/go-version.txt"
for o in a b; do
  mkdir -p "$E/harness-run-copy/$o"
  for f in files.sha256 harness.diff differs-from-archive.txt runcopy-against-commit.txt; do cp $R/overlay-$o/$f "$E/harness-run-copy/$o/" 2>/dev/null; done
  cp $L/suite-$o.txt "$E/harness-run-copy/$o/suite.txt"; cp $L/suite-$o-notok.txt "$E/harness-run-copy/$o/suite-notok.txt"
done
cmp -s $L/suite-a-notok.txt $L/suite-b-notok.txt && echo "the not-OK tests of copy a and copy b are the same by name" > "$E/harness-run-copy/suites-compared.txt" || echo "the not-OK lists DIFFER" > "$E/harness-run-copy/suites-compared.txt"
for f in deploy/e2e/release-recovery/set9_trial.py deploy/e2e/release-recovery/run-set9.sh web/tools/browser-inspect/cold-load-set9.mjs; do
  printf '%s  working tree %s\n' "$(sed 's/\r$//' "$REPO/$f" | sha256sum | cut -c1-64)" "$f"
done > "$E/harness-run-copy/working-tree-against-copy-b.txt"
cat $R/overlay-b/files.sha256 >> "$E/harness-run-copy/working-tree-against-copy-b.txt"
cp $R/progress.txt "$E/host/progress.txt"; cp $R/c-drive-cells.txt "$E/host/c-drive-cells.txt"
cp $J/c-drive-watch.txt "$E/host/c-drive-watch.txt"; cp $J/keepawake.log "$E/host/keepawake.log" 2>/dev/null; cp $J/sleep-events.txt "$E/host/sleep-events.txt" 2>/dev/null
{ echo "qemu: $(qemu-system-x86_64 --version | head -n 1)"; echo "kernel (WSL): $(uname -r)"; echo "python: $(python3 --version)"; } > "$E/host/hostcheck.txt"
G -C "$REPO" status --short > "$E/host/working-tree-status.txt"
G -C "$REPO" rev-parse HEAD > "$E/host/head-at-staging.txt"
echo "staged common at $(date -u +%FT%TZ)"
