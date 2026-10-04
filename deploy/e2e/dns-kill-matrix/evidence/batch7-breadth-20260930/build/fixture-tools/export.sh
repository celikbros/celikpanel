set -euo pipefail
# Copies all retained cell evidence and build records into the repository evidence directory (untracked).
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930'
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch7
ROOT=/var/tmp/cp-b7-0930
test ! -e "$D"
mkdir -p "$D/build/fixture-tools"
while read s c; do
  if [ ! -d $ROOT/evidence/$s ]; then echo "$s: no evidence directory"; continue; fi
  mkdir -p "$D/$s"
  (cd $ROOT/evidence/$s && tar -cf - --exclude=./raw/results .) | tar -C "$D/$s" --no-same-owner --no-same-permissions -xf -
  if [ -d $ROOT/evidence/$s/raw/results/$c ]; then
    mkdir -p "$D/$s/raw/results"
    (cd $ROOT/evidence/$s/raw/results/$c && tar -cf - .) | tar -C "$D/$s/raw/results" --no-same-owner --no-same-permissions -xf -
  fi
  if [ -f $ROOT/evidence/$s/raw/results/$c/result.json ]; then
    python3 $SP/boundcheck.py $ROOT/evidence/$s $c > "$D/$s/boundary-window.txt"
    python3 $SP/digest.py $ROOT/evidence/$s $c > "$D/$s/cell-digest.txt"
  fi
  echo "$s files: $(find "$D/$s" -type f | wc -l)"
done < $SP/cells.txt
cp /var/tmp/cp-b7-build.log "$D/build/build.log"
cp /var/tmp/cp-b7-setup.log "$D/build/work-root-setup.log"
cp /var/tmp/cp-b7-webdist.sha256 "$D/build/web-dist.sha256"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$D/build/go-version.txt"
(cd /root/cp-b7-artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts.sha256"
for r in $(cat $SP/roots.txt); do
  n=$(echo $r | sed 's#/var/tmp/cp-b7-0930#root1#; s#root1/#root1-#')
  (cd $r/artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts-$n.sha256"
  cmp "$D/build/artifacts.sha256" "$D/build/artifacts-$n.sha256" && echo "artifact copy $n identical"
done
(cd /root/cp-b7-src && sha256sum deploy/e2e/dns-kill-matrix/fixture.py deploy/e2e/dns-kill-matrix/guest_bootstrap.py deploy/e2e/dns-kill-matrix/guest_bootstrap.sh deploy/e2e/dns-kill-matrix/run_cell.py deploy/e2e/dns-kill-matrix/guest_recovery_probe.py deploy/e2e/dns-kill-matrix/native_primary_peer.py deploy/e2e/dns-kill-matrix/native_primary_peer_probe.py deploy/e2e/dns-kill-matrix/manifest.json cmd/agent/dns_engine_kill_matrix_linux.go) > "$D/build/harness-files.sha256"
(cd /root/cp-b7-tools && sha256sum oi-smstatus overlay.json smstatus.go) > "$D/build/fixture-tools/oi-smstatus.sha256"
cp /root/cp-b7-tools/overlay.json "$D/build/fixture-tools/overlay.json"
for f in $SP/*.sh $SP/*.py $SP/*.txt $SP/smstatus.go; do b=$(basename $f); [ "$b" = hostcheck.sh ] && continue; cp $f "$D/build/fixture-tools/$b"; done
[ -f $SP/harness-workarounds.diff ] && cp $SP/harness-workarounds.diff "$D/build/harness-workarounds.diff"
mkdir -p /var/tmp/cp-b7-verify && git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' archive dbd6a6b6 | tar -x -C /var/tmp/cp-b7-verify
set +e; diff -r --exclude=web /var/tmp/cp-b7-verify /root/cp-b7-src > "$D/build/source-tree-vs-archive.diff" ; echo "diff rc=$? (web/dist excluded; it is untracked)" | tee -a "$D/build/source-tree-vs-archive.diff"; set -e
rm -rf /var/tmp/cp-b7-verify
echo "longest path (repo-relative):"; (cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' && find deploy/e2e/dns-kill-matrix/evidence/batch7-breadth-20260930 -type f | awk '{print length($0), $0}' | sort -n | tail -1)
echo "longest windows path: $(( $(find "$D" -type f | awk '{print length($0)}' | sort -n | tail -1) - 4 ))"
du -sh "$D"; find "$D" -type f | wc -l
