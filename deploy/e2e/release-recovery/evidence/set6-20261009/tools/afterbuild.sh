#!/bin/bash
# set6, after the builds: (1) the read-only host proof of every archive of both artifacts documents (inventory,
# release policy, committed source), (2) the dry run of each of the six cells (no lab is created), (3) the removal
# of the build intermediates THIS run created: the `src` directory (the extracted source with its compiled
# binaries) of each dist directory these builds made. The archive, dist.json, the build log and product-web-src of
# each dist stay, and so do the builders' clones (the cells read these and nothing else).
# The reused alpha.81 dist (built by set3) is not touched.
# usage: afterbuild.sh COPY
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
copy=$1
R=/var/tmp/cp-set6-run; L=$R/logs
H=$R/harness-$copy/deploy/e2e/release-recovery
CUR=$(tail -n 2 $L/build-cur.out | grep -m1 upd1-artifacts.json)
A81=$(tail -n 2 $L/build-a81.out | grep -m1 upd1-artifacts.json)
[ -f "$CUR" ] && [ -f "$A81" ] || { echo "an artifacts document is missing"; exit 2; }
echo "$CUR" > $R/artifacts-cur.path; echo "$A81" > $R/artifacts-a81.path
mkdir -p $R/build
rc1=0
for b in cur a81; do
  art=$(cat $R/artifacts-$b.path)
  bash $H/run-upd1.sh prove "$art" > $R/build/$b-prove.json 2> $R/build/$b-prove.stderr.txt; r=$?
  echo "prove $b rc=$r"; echo "$(date -u +%FT%TZ) prove $b rc=$r" >> $R/progress.txt
  [ $r -eq 0 ] || rc1=1
done
rc2=0
for c in set6-arch set6-debian13 set6-ubuntu; do
  bash $H/run-set6.sh dry-run $c "$CUR" dry-$c > $R/build/dry-$copy-$c.json 2> $R/build/dry-$copy-$c.stderr.txt; r=$?
  echo "dry $c rc=$r steps=$(python3 -I -c "import json,sys;print(len(json.load(open(sys.argv[1])).get('steps') or []))" $R/build/dry-$copy-$c.json 2>/dev/null)"; [ $r -eq 0 ] || rc2=1
  echo "$r" > $R/build/dry-$copy-$c.rc.txt
done
for c in upd1-arch-good upd1-debian13-good upd1-ubuntu-good; do
  bash $H/run-set6.sh dry-run $c "$A81" dry-$c > $R/build/dry-$copy-$c.json 2> $R/build/dry-$copy-$c.stderr.txt; r=$?
  echo "dry $c rc=$r added=$(python3 -I -c "import json,sys;print(json.load(open(sys.argv[1])).get('added_steps'))" $R/build/dry-$copy-$c.json 2>/dev/null)"; [ $r -eq 0 ] || rc2=1
  echo "$r" > $R/build/dry-$copy-$c.rc.txt
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
[ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] || { echo "proof or dry run failed: nothing is removed"; exit 1; }
[ "${2:-}" = keep ] && { echo "dry runs only: nothing is removed"; exit 0; }
: > $R/build/dist-dirs.txt
for b in cur a81; do
python3 -I - "$(cat $R/artifacts-$b.path)" $b >> $R/build/dist-dirs.txt <<'PY'
import json, os, re, sys
d = json.load(open(sys.argv[1]))
for role in ("baseline", "good", "defective", "startcheck", "realstart"):
    if role in d:
        path = os.path.dirname(d[role]["archive"])
        assert re.fullmatch(r"/var/tmp/cp-pair-accept/dist/[0-9a-f]{40}-acceptance-license", path), path
        print(sys.argv[2] + "-" + role, "reused" if d[role].get("reused_dist") else "built-by-this-run", path)
PY
done
cat $R/build/dist-dirs.txt
while read -r role origin path; do
  [ "$origin" = built-by-this-run ] || continue
  s="$path/src"
  [ -d "$s" ] || continue
  # only a directory these builds wrote: its dist.json is younger than the build job's start
  [ "$path/dist.json" -nt $L/build.start ] || { echo "KEPT $s (dist.json is not younger than this run's build start)"; continue; }
  b=$(du -sb "$s" | cut -f1)
  rm -rf -- "${s:?}"
  echo "$(date -u +%FT%TZ) removed $s bytes=$b (build intermediate of this run's $role dist: extracted source and compiled binaries; the archive, dist.json, build.log and product-web-src stay)" | tee -a $R/removals-build.txt
done < $R/build/dist-dirs.txt
du -sh /var/tmp/cp-pair-accept/dist/*-acceptance-license 2>/dev/null | sort -k2
du -sh "$(dirname "$CUR")" "$(dirname "$A81")"
