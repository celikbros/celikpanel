set -euo pipefail
# Copies all retained cell evidence and build records into the repository evidence directory (untracked).
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch6a-fixed-hook-20260929'
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch6a
ROOT=/var/tmp/cp-b6a-0929
test ! -e "$D"
mkdir -p "$D/build/fixture-tools"
while read s c; do
  test -d $ROOT/evidence/$s
  mkdir -p "$D/$s"
  (cd $ROOT/evidence/$s && tar -cf - --exclude=./raw/results .) | tar -C "$D/$s" --no-same-owner --no-same-permissions -xf -
  mkdir -p "$D/$s/raw/results"
  (cd $ROOT/evidence/$s/raw/results/$c && tar -cf - .) | tar -C "$D/$s/raw/results" --no-same-owner --no-same-permissions -xf -
  python3 $SP/boundcheck.py $ROOT/evidence/$s $c > "$D/$s/boundary-window.txt"
  python3 $SP/digest.py $ROOT/evidence/$s $c > "$D/$s/cell-digest.txt"
  echo "$s files: $(find "$D/$s" -type f | wc -l)"
done <<'L'
c01-fresh-bind-reboot bind__target-verified__after-write__standalone__peer-reachable
c02-fresh-bind-arch bind__target-verified__before-write__standalone__peer-reachable
c03-fresh-pdns-pre pdns-switch__target-staged__after-write__standalone__peer-reachable
c04-fresh-pdns-post pdns-switch__target-started__after-write__standalone__peer-reachable
c05-v2-prestart bind__target-staged__after-write__standalone__peer-reachable
c06-v2-poststop bind__target-started__after-write__standalone__peer-reachable
c07-bind-adopt bind__rolling-back__after-write__standalone__peer-reachable
c08-pdns-adopt-rb pdns-adopt__rolled-back__after-write__standalone__peer-reachable
c09-pdns-adopt-intent pdns-adopt__intent__after-write__standalone__peer-reachable
c10-takeover bind__target-staged__after-write__standalone__peer-reachable
c11-bindsec-bindpri bind__target-verified__after-write__paired-secondary__peer-reachable
c12-bindsec-pdnsnative bind__target-verified__after-write__paired-secondary__peer-reachable
L
# build records
cp /var/tmp/cp-b6a-build.log "$D/build/build.log"
cp /var/tmp/cp-b6a-setup.log "$D/build/work-root-setup.log"
cp /var/tmp/cp-b6a-webdist.sha256 "$D/build/web-dist.sha256"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version > "$D/build/go-version.txt"
(cd /root/cp-b6a-artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts.sha256"
(cd /var/tmp/cp-b6a-0929/artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts-root1.sha256"
(cd /var/tmp/cp-b6a-0929/r2/artifacts && sha256sum agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge && find recovery-runtime -type f -print0 | sort -z | xargs -0 sha256sum) > "$D/build/artifacts-r2.sha256"
cmp "$D/build/artifacts.sha256" "$D/build/artifacts-root1.sha256" && cmp "$D/build/artifacts.sha256" "$D/build/artifacts-r2.sha256" && echo "artifact copies identical"
(cd /root/cp-b6a-src && sha256sum deploy/e2e/dns-kill-matrix/fixture.py deploy/e2e/dns-kill-matrix/guest_bootstrap.py deploy/e2e/dns-kill-matrix/guest_bootstrap.sh deploy/e2e/dns-kill-matrix/run_cell.py deploy/e2e/dns-kill-matrix/guest_recovery_probe.py deploy/e2e/dns-kill-matrix/native_primary_peer.py deploy/e2e/dns-kill-matrix/native_primary_peer_probe.py deploy/e2e/dns-kill-matrix/manifest.json cmd/agent/dns_engine_kill_matrix_linux.go) > "$D/build/harness-files.sha256"
(cd /root/cp-b6a-tools && sha256sum oi-smstatus overlay.json smstatus.go) > "$D/build/fixture-tools/oi-smstatus.sha256"
cp /root/cp-b6a-tools/overlay.json "$D/build/fixture-tools/overlay.json"
for f in build.sh setup.sh cell.sh runonly.sh runcell.sh collect.sh down.sh stoponly.sh gssh.py scpcmd.py watcher.sh guesttar_remote.sh ownerpost_remote.sh unitfacts_remote.sh versions_remote.sh peerfacts_remote.sh peerjournal_remote.sh peerloop_start_remote.sh secstate_remote.sh pairq.py dnsq.py soaq.py extract.py extradiag.sh extradiag_remote.sh smstatus.go boundcheck.py bc.sh digest.py dg.sh summ.sh check.sh peerv.sh poll.sh waitfor.sh export.sh; do cp $SP/$f "$D/build/fixture-tools/$f"; done
# cp-b6a-src must be byte-identical to git archive 94cd124b (no harness workaround was applied)
mkdir -p /var/tmp/cp-b6a-verify && git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' archive 94cd124b | tar -x -C /var/tmp/cp-b6a-verify
diff -r --exclude=web /var/tmp/cp-b6a-verify /root/cp-b6a-src > "$D/build/source-tree-vs-archive.diff" && echo "cp-b6a-src identical to git archive 94cd124b (web/dist excluded; it is untracked)" | tee -a "$D/build/source-tree-vs-archive.diff"
rm -rf /var/tmp/cp-b6a-verify
echo "longest path (repo-relative):"; (cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' && find deploy/e2e/dns-kill-matrix/evidence/batch6a-fixed-hook-20260929 -type f | awk '{print length($0), $0}' | sort -n | tail -1)
echo "longest windows path: $(( $(find "$D" -type f | awk '{print length($0)}' | sort -n | tail -1) - 4 ))"
du -sh "$D"; find "$D" -type f | wc -l
