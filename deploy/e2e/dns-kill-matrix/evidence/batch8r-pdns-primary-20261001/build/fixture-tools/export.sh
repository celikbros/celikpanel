set -euo pipefail
# Copies all retained cell evidence and build records into the repository evidence directory (untracked).
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
D="$REPO/deploy/e2e/dns-kill-matrix/evidence/batch8r-pdns-primary-20261001"
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r
ROOT=/var/tmp/cp-b8r-1001
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
cp /var/tmp/cp-b8r-build.log "$D/build/build.log"
cp /var/tmp/cp-b8r-webbuild.log "$D/build/web-build.log"
cp /var/tmp/cp-b8r-setup.log "$D/build/work-root-setup.log"
cp /var/tmp/cp-b8r-webdist.sha256 "$D/build/web-dist.sha256"
cp $ROOT/logs/chain.log "$D/build/chain.log"
cp /var/tmp/cp-b8r-1001/status-texts-all-cells.txt "$D/build/status-texts-all-cells.txt"
cp $SP/repo-head-at-start.txt "$D/build/repo-head-at-start.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$D/build/go-version.txt"
(cd /root/cp-b8r-artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge dns-owner-tools/* && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts.sha256"
for r in $(cat $SP/roots.txt); do
  n=$(echo $r | sed 's#^/var/tmp/cp-b8r-1001$#root1#; s#^/var/tmp/cp-b8r-1001/##')
  (cd $r/artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge dns-owner-tools/* && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts-$n.sha256"
  cmp "$D/build/artifacts.sha256" "$D/build/artifacts-$n.sha256" && echo "artifact copy $n identical"
done
(cd /root/cp-b8r-src && sha256sum deploy/e2e/dns-kill-matrix/fixture.py deploy/e2e/dns-kill-matrix/guest_bootstrap.py deploy/e2e/dns-kill-matrix/guest_bootstrap.sh deploy/e2e/dns-kill-matrix/run_cell.py deploy/e2e/dns-kill-matrix/guest_recovery_probe.py deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py deploy/e2e/dns-kill-matrix/native_pdns_peer_probe.py deploy/e2e/dns-kill-matrix/manifest.json cmd/agent/dns_engine_kill_matrix_linux.go cmd/agent/dns_engine_pdns_switch.go cmd/panel/dns_engine.go) > "$D/build/harness-files.sha256"
(cd /root/cp-b8r-tools && sha256sum oi-smstatus overlay.json smstatus.go) > "$D/build/fixture-tools/oi-smstatus.sha256"
cp /root/cp-b8r-tools/overlay.json "$D/build/fixture-tools/overlay.json"
for f in $SP/*.sh $SP/*.py $SP/*.txt $SP/smstatus.go; do b=$(basename $f); [ "$b" = hostcheck.sh ] && continue; cp $f "$D/build/fixture-tools/$b"; done
mkdir -p /var/tmp/cp-b8r-verify && git -c safe.directory='*' -C "$REPO" archive e3591875 | tar -x -C /var/tmp/cp-b8r-verify
set +e; diff -r --exclude=dist /var/tmp/cp-b8r-verify /root/cp-b8r-src > "$D/build/source-tree-vs-archive.diff" ; echo "diff rc=$? (web/dist excluded: built from the archive's web/ during this run, not tracked)" | tee -a "$D/build/source-tree-vs-archive.diff"; set -e
rm -rf /var/tmp/cp-b8r-verify
echo "repository HEAD at export (record only): $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD) $(date -u +%FT%TZ)" | tee "$D/build/repo-head-at-end.txt"
echo "longest path (repo-relative):"; (cd "$REPO" && find deploy/e2e/dns-kill-matrix/evidence/batch8r-pdns-primary-20261001 -type f | awk '{print length($0), $0}' | sort -n | tail -1)
du -sh "$D"; find "$D" -type f | wc -l
