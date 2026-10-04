set -euo pipefail
# Copies all retained cell evidence and build records into the repository evidence directory (untracked).
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
D="$REPO/deploy/e2e/dns-kill-matrix/evidence/batch9-pdns-primary-zero-zone-20261001"
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
ROOT=/var/tmp/cp-b9-1001
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
  echo "$s files: $(find "$D/$s" -type f | wc -l)"
done < $SP/cells.txt
# z04 second collect (after the owner enrollment and the refused recover): same flattening of raw/results
s=z04-zero-committed-zl/rec; c=pdns-switch__committed__after-write__paired-primary__peer-reachable
rm -rf "$D/$s"; mkdir -p "$D/$s"
(cd $ROOT/evidence/$s && tar -cf - --exclude=./raw/results .) | tar -C "$D/$s" --no-same-owner --no-same-permissions -xf -
mkdir -p "$D/$s/raw/results"
(cd $ROOT/evidence/$s/raw/results/$c && tar -cf - .) | tar -C "$D/$s/raw/results" --no-same-owner --no-same-permissions -xf -
echo "$s files: $(find "$D/$s" -type f | wc -l)"
cp /var/tmp/cp-b9-build.log "$D/build/build.log"
cp /var/tmp/cp-b9-webbuild.log "$D/build/web-build.log"
cp /var/tmp/cp-b9-setup.log "$D/build/work-root-setup.log"
cp /var/tmp/cp-b9-webdist.sha256 "$D/build/web-dist.sha256"
cp $ROOT/logs/chain.log "$D/build/chain.log"
cp /var/tmp/cp-b9-triggertest.log "$D/build/trigger-tests.log"
cp $SP/offline-test-counts.txt "$D/build/offline-test-counts.txt"
cp /var/tmp/cp-b9-1001/status-texts-all-cells.txt "$D/build/status-texts-all-cells.txt"
cp /var/tmp/cp-b9-1001/remaining-on-host.txt "$D/build/remaining-on-host.txt"
cp $SP/repo-head-at-start.txt "$D/build/repo-head-at-start.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$D/build/go-version.txt"
(cd /root/cp-b9-artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge dns-owner-tools/* && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts.sha256"
for r in $(cat $SP/roots.txt); do
  n=$(echo $r | sed 's#^/var/tmp/cp-b9-1001$#root1#; s#^/var/tmp/cp-b9-1001/##')
  (cd $r/artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge dns-owner-tools/* && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts-$n.sha256"
  cmp "$D/build/artifacts.sha256" "$D/build/artifacts-$n.sha256" && echo "artifact copy $n identical"
done
(cd /root/cp-b9-src && sha256sum deploy/e2e/dns-kill-matrix/fixture.py deploy/e2e/dns-kill-matrix/guest_bootstrap.py deploy/e2e/dns-kill-matrix/guest_bootstrap.sh deploy/e2e/dns-kill-matrix/run_cell.py deploy/e2e/dns-kill-matrix/guest_recovery_probe.py deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py deploy/e2e/dns-kill-matrix/native_pdns_peer_probe.py deploy/e2e/dns-kill-matrix/manifest.json cmd/agent/dns_engine_kill_matrix_linux.go cmd/agent/dns_engine_pdns_switch.go cmd/panel/dns_engine.go) > "$D/build/harness-files.sha256"
(cd /root/cp-b9-tools && sha256sum oi-smstatus overlay.json smstatus.go) > "$D/build/fixture-tools/oi-smstatus.sha256"
cp /root/cp-b9-tools/overlay.json "$D/build/fixture-tools/overlay.json"
for f in $SP/*.sh $SP/*.py $SP/*.txt; do b=$(basename $f); cp $f "$D/build/fixture-tools/$b"; done
cp /root/cp-b9-tools/smstatus.go "$D/build/fixture-tools/smstatus.go"
set +e; ( cd "$D/build/fixture-tools" && diff -ru "$REPO/deploy/e2e/dns-kill-matrix/evidence/batch8r-pdns-primary-20261001/build/fixture-tools" . ) > "$D/build/fixture-tools-vs-batch8r.diff"; echo "fixture diff rc=$?"; set -e
mkdir -p /var/tmp/cp-b9-verify && git -c safe.directory='*' -C "$REPO" archive 3cceb29a | tar -x -C /var/tmp/cp-b9-verify
set +e; diff -r --exclude=dist /var/tmp/cp-b9-verify /root/cp-b9-src > "$D/build/source-tree-vs-archive.diff" ; echo "diff rc=$? (web/dist excluded: built from the archive's web/ during this run, not tracked)" | tee -a "$D/build/source-tree-vs-archive.diff"; set -e
rm -rf /var/tmp/cp-b9-verify
echo "repository HEAD at export (record only): $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD) $(date -u +%FT%TZ)" | tee "$D/build/repo-head-at-end.txt"
echo "longest path (repo-relative):"; (cd "$REPO" && find deploy/e2e/dns-kill-matrix/evidence/batch9-pdns-primary-zero-zone-20261001 -type f | awk '{print length($0), $0}' | sort -n | tail -1)
du -sh "$D"; find "$D" -type f | wc -l
