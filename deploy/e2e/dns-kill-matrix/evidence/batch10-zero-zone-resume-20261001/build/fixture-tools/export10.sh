set -euo pipefail
# Copies the retained cell evidence and build records into the repository evidence directory (untracked, new).
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
D="$REPO/deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001"
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
ROOT=/var/tmp/cp-b10-1001
test ! -e "$D"
mkdir -p "$D/build/fixture-tools"
bash $SP/remain10.sh > $ROOT/remaining-on-host.txt 2>&1
for s in z04-resume z05-held-resume; do
  L=$ROOT/logs/$s; E=$ROOT/evidence/$s
  for f in $L/*.txt $L/*.log $L/*.json $L/*.rc; do [ -e "$f" ] && cp -p "$f" $E/; done
  [ -d $L/pre-boot ] && { mkdir -p $E/pre-boot; cp -p $L/pre-boot/* $E/pre-boot/; }
  mkdir -p "$D/$s"
  (cd $E && tar -cf - --exclude=./raw/results .) | tar -C "$D/$s" --no-same-owner --no-same-permissions -xf -
  c=$(ls $E/raw/results)
  mkdir -p "$D/$s/raw/results"
  (cd $E/raw/results/$c && tar -cf - .) | tar -C "$D/$s/raw/results" --no-same-owner --no-same-permissions -xf -
  echo "$s files: $(find "$D/$s" -type f | wc -l) (raw/results from $c)"
done
cp /var/tmp/cp-b10-build.log "$D/build/build.log"
cp /var/tmp/cp-b10-webbuild.log "$D/build/web-build.log"
cp /var/tmp/cp-b10-setup.log "$D/build/work-root-setup.log"
cp /var/tmp/cp-b10-webdist.sha256 "$D/build/web-dist.sha256"
cp $ROOT/logs/chain.log "$D/build/chain.log"
cp /var/tmp/cp-b10-triggertest.log "$D/build/trigger-tests.log"
cp $SP/offline-test-counts.txt "$D/build/offline-test-counts.txt"
cp $ROOT/remaining-on-host.txt "$D/build/remaining-on-host.txt"
cp $SP/repo-head-at-start.txt "$D/build/repo-head-at-start.txt"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$D/build/go-version.txt"
(cd /root/cp-b10-artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge dns-owner-tools/* && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts.sha256"
(cd $ROOT/r1/artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge dns-owner-tools/* && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts-r1.sha256"
cmp "$D/build/artifacts.sha256" "$D/build/artifacts-r1.sha256" && echo "artifact copy r1 identical"
(cd /root/cp-b10-src && sha256sum deploy/e2e/dns-kill-matrix/fixture.py deploy/e2e/dns-kill-matrix/guest_bootstrap.py deploy/e2e/dns-kill-matrix/guest_bootstrap.sh deploy/e2e/dns-kill-matrix/run_cell.py deploy/e2e/dns-kill-matrix/guest_recovery_probe.py deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py deploy/e2e/dns-kill-matrix/native_pdns_peer_probe.py deploy/e2e/dns-kill-matrix/manifest.json cmd/agent/dns_engine_kill_matrix_linux.go cmd/agent/dns_engine_pdns_switch.go cmd/panel/dns_engine.go cmd/dns-kill-matrix-trigger/pdns_fresh_primary_v3.go cmd/dns-kill-matrix-trigger/zone_delete_trial.go) > "$D/build/harness-files.sha256"
(cd /root/cp-b10-tools && sha256sum oi-smstatus overlay.json smstatus.go) > "$D/build/fixture-tools/oi-smstatus.sha256"
cp /root/cp-b10-tools/overlay.json /root/cp-b10-tools/smstatus.go "$D/build/fixture-tools/"
for f in $SP/*.sh $SP/*.py $SP/*.txt; do cp $f "$D/build/fixture-tools/$(basename $f)"; done
set +e; ( cd "$D/build/fixture-tools" && diff -ru "$REPO/deploy/e2e/dns-kill-matrix/evidence/batch9-pdns-primary-zero-zone-20261001/build/fixture-tools" . ) > "$D/build/fixture-tools-vs-batch9.diff"; echo "fixture diff rc=$?"; set -e
mkdir -p /var/tmp/cp-b10-verify && git -c safe.directory='*' -C "$REPO" archive 0d4c0324 | tar -x -C /var/tmp/cp-b10-verify
set +e; diff -r --exclude=dist /var/tmp/cp-b10-verify /root/cp-b10-src > "$D/build/source-tree-vs-archive.diff" ; echo "diff rc=$? (web/dist excluded: built from the archive's web/ during this run, not tracked)" | tee -a "$D/build/source-tree-vs-archive.diff"; set -e
rm -rf /var/tmp/cp-b10-verify
echo "repository HEAD at export (record only): $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD) $(date -u +%FT%TZ)" | tee "$D/build/repo-head-at-end.txt"
for f in "$D"/*/raw/state/dns-engine-switch-v3-archive-*.json; do
  [ -e "$f" ] || continue
  dir=$(dirname "$f"); base=$(basename "$f")
  rid=$(echo "$base" | sed -E 's/^dns-engine-switch-v3-archive-([0-9a-f]{32})-[0-9a-f]{64}\.json$/\1/')
  new="v3-archive-$rid.json"; test ! -e "$dir/$new"
  sum=$(sha256sum "$f" | cut -c1-64); mv "$f" "$dir/$new"
  echo "original=$base renamed=$new sha256=$sum (renamed only to keep the repository path under 200 characters; bytes unchanged)" >> "$dir/renamed-files.txt"
done
cd "$REPO"
echo "longest repo-relative path:"; find deploy/e2e/dns-kill-matrix/evidence/batch10-zero-zone-resume-20261001 -type f | awk '{print length($0), $0}' | sort -n | tail -1
du -sh "$D"; find "$D" -type f | wc -l
echo EXPORT-DONE
