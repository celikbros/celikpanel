#!/bin/bash
# set8: stage what is common to the run (build, proof, dry runs, run copies, offline suites, host records, tools).
set -uo pipefail
R=/var/tmp/cp-set8-run; L=$R/logs
NAME=$(cat $R/evidence-name.txt)
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$NAME"
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
G() { git -c safe.directory='*' "$@"; }
SRC=$(G -C "$REPO" rev-parse '2a0af8866^{commit}')
mkdir -p "$E/build/cur" "$E/harness-run-copy" "$E/host" "$E/tools"
field() { python3 -I -c "import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],{}).get(sys.argv[3],''))" "$1" "$2" "$3"; }
ART=$(cat $R/artifacts-cur.path); B=$(dirname "$ART")
cp "$ART" "$E/build/cur/"
for r in baseline good; do
  d=$(field "$ART" $r dist_json)
  [ -n "$d" ] && [ -f "$d" ] && cp "$d" "$E/build/cur/dist-$r.json"
done
G -C $B/repo for-each-ref --format='%(refname) %(objectname) tree=%(tree) %(subject)' refs/upd1 > "$E/build/cur/fixture-commits.txt"
base=$(field "$ART" baseline commit)
{
  echo "source 2a0af8866 = $SRC tree $(G -C "$REPO" rev-parse "$SRC^{tree}") (v0.1.0-alpha.82: $(G -C "$REPO" rev-parse 'v0.1.0-alpha.82^{commit}'))"
  echo "baseline (installed fresh by every cell) $base tree $(G -C $B/repo rev-parse "$base^{tree}")"
  echo "### files that differ between the source 2a0af8866 and the baseline $base"
  G -C $B/repo diff --name-status $SRC $base
  echo "### files of cmd/, internal/, web/ that differ between them: $(G -C $B/repo diff --name-only $SRC $base -- cmd internal web | wc -l)"
} > "$E/build/cur/trees.txt"
G -C $B/repo diff $SRC $base > "$E/build/cur/fixture-patch-baseline.diff"
cp $R/build/cur-prove.json "$E/build/cur-prove.json"
cp $R/build/dry-*.json $R/build/dry-*.rc.txt "$E/build/" 2>/dev/null
tail -n 40 $L/build-cur.err > "$E/build/build-cur.err.tail.txt"; cp $L/build-cur.out "$E/build/build-cur.out.txt"
cp $L/build.out "$E/build/build-job.out.txt"; cp $L/build.start "$E/build/build-job.start.txt"; cp $L/build.end "$E/build/build-job.end.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$E/build/go-version.txt"
for o in a b c d; do
  mkdir -p "$E/harness-run-copy/$o"
  for f in files.sha256 harness.diff differs-from-archive.txt runcopy-against-commit.txt; do cp $R/overlay-$o/$f "$E/harness-run-copy/$o/" 2>/dev/null; done
  cp $L/suite-$o.txt "$E/harness-run-copy/$o/suite.txt"; cp $L/suite-$o-notok.txt "$E/harness-run-copy/$o/suite-notok.txt"
done
for o in b c d; do cmp -s $L/suite-a-notok.txt $L/suite-$o-notok.txt && echo "copy $o: the five not-OK tests are those of copy a by name" || echo "copy $o: the not-OK list DIFFERS from copy a"; done > "$E/harness-run-copy/suites-compared.txt"
for f in set8_trial.py guest_set8_native.py run-set8.sh test_set8_trial.py; do
  printf '%s  working tree %s\n' "$(sed 's/\r$//' "$REPO/deploy/e2e/release-recovery/$f" | sha256sum | cut -c1-64)" "$f"
done > "$E/harness-run-copy/working-tree-against-copy-d.txt"
cat $R/overlay-d/files.sha256 >> "$E/harness-run-copy/working-tree-against-copy-d.txt"
cp $R/progress.txt "$E/host/progress.txt"; cp $R/c-drive-cells.txt "$E/host/c-drive-cells.txt"
cp $J/c-drive-watch.txt "$E/host/c-drive-watch.txt"; cp $J/c-drive.txt "$E/host/c-drive.txt" 2>/dev/null; cp $J/keepawake.log "$E/host/keepawake.log" 2>/dev/null; cp $J/sleep-events.txt "$E/host/sleep-events.txt" 2>/dev/null
{ echo "qemu: $(qemu-system-x86_64 --version | head -n 1)"; echo "kernel (WSL): $(uname -r)"; echo "python: $(python3 --version)"; cat $L/heads.txt | sed 's/^/rev: /'; } > "$E/host/hostcheck.txt"
G -C "$REPO" status --short > "$E/host/working-tree-status.txt"
G -C "$REPO" rev-parse HEAD > "$E/host/head-at-staging.txt"
G -C "$REPO" diff --stat 2a0af8866 HEAD -- cmd internal web/src > "$E/host/product-diff-2a0af8866-to-head.txt"; echo "(empty above = no product file differs)" >> "$E/host/product-diff-2a0af8866-to-head.txt"
for f in $J/*; do case $(basename "$f") in c-drive*|keepawake.log|sleep-events.txt|cwatch.stop|keepawake.stop) ;; *) cp "$f" "$E/tools/";; esac; done
echo "staged common at $(date -u +%FT%TZ)"
