#!/bin/bash
# set5, after the build: (1) the read-only host proof of every archive of the artifacts document (inventory,
# release policy, committed source), (2) the dry run of each of the ten cells (no lab is created), (3) the removal
# of the build intermediates THIS run created: the `src` directory (the extracted source with its compiled
# binaries, about 0.9 GB each) of each dist directory this build made. The archive, dist.json, the build log and
# product-web-src of each dist stay, and so does the builder's clone (the cells read these and nothing else).
# The reused alpha.81 dist (built by set3) is not touched.
# usage: afterbuild.sh COPY
export PYTHONDONTWRITEBYTECODE=1
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:$PATH
copy=$1
R=/var/tmp/cp-set5-run; L=$R/logs
H=$R/harness-$copy/deploy/e2e/release-recovery
ART=$(tail -n 2 $L/build-a81.out | grep -m1 upd1-artifacts.json)
[ -f "$ART" ] || { echo "no artifacts document"; exit 2; }
echo "$ART" > $R/artifacts.path
mkdir -p $R/build
bash $H/run-upd1.sh prove "$ART" > $R/build/a81-prove.json 2> $R/build/a81-prove.stderr.txt; rc1=$?
echo "prove a81 rc=$rc1"
echo "$(date -u +%FT%TZ) prove a81 rc=$rc1" >> $R/progress.txt
rc2=0
for c in upd1-debian13-good upd1-ubuntu-good upd1-arch-good upd1-debian13-defective upd1-ubuntu-defective upd1-arch-defective \
         upd1-debian13-startcheck upd1-debian13-owner-continuation upd1-ubuntu-owner-continuation upd1-debian13-mgmt-off-reboot; do
  bash $H/run-set5.sh dry-run $c "$ART" dry-$c > $R/build/dry-$copy-$c.json 2> $R/build/dry-$copy-$c.stderr.txt; r=$?
  echo "dry $c rc=$r added=$(python3 -I -c "import json,sys;print(json.load(open(sys.argv[1])).get('added_steps'))" $R/build/dry-$copy-$c.json 2>/dev/null)"; [ $r -eq 0 ] || rc2=1
done
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab was created"
[ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] || { echo "proof or dry run failed: nothing is removed"; exit 1; }
python3 -I - "$ART" > $R/build/dist-dirs.txt <<'PY'
import json, os, re, sys
d = json.load(open(sys.argv[1]))
for role in ("baseline", "good", "defective", "startcheck", "realstart"):
    if role in d:
        path = os.path.dirname(d[role]["archive"])
        assert re.fullmatch(r"/var/tmp/cp-pair-accept/dist/[0-9a-f]{40}-acceptance-license", path), path
        print(role, "reused" if d[role].get("reused_dist") else "built-by-this-run", path)
PY
cat $R/build/dist-dirs.txt
while read -r role origin path; do
  [ "$origin" = built-by-this-run ] || continue
  s="$path/src"
  [ -d "$s" ] || continue
  # only a directory this build wrote: its dist.json is younger than the build's start
  [ "$path/dist.json" -nt $L/build.start ] || { echo "KEPT $s (dist.json is not younger than this run's build start)"; continue; }
  b=$(du -sb "$s" | cut -f1)
  rm -rf -- "${s:?}"
  echo "$(date -u +%FT%TZ) removed $s bytes=$b (build intermediate of this run's $role dist: extracted source and compiled binaries; the archive, dist.json, build.log and product-web-src stay)" | tee -a $R/removals-build.txt
done < $R/build/dist-dirs.txt
du -sh /var/tmp/cp-pair-accept/dist/*-acceptance-license 2>/dev/null | sort -k2 | grep -F -f <(awk '{print $3}' $R/build/dist-dirs.txt)
du -sh "$(dirname "$ART")"
