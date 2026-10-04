set -euo pipefail
exec > /var/tmp/cp-b6a-build.log 2>&1
date -u +%FT%TZ
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
SRC=/root/cp-b6a-src
A=/root/cp-b6a-artifacts
T=/root/cp-b6a-tools
test ! -e "$SRC"; test ! -e "$A"; test ! -e "$T"; test ! -e /var/tmp/cp-b6a-0929
echo "repository HEAD at run time: $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD)"
git -c safe.directory='*' -C "$REPO" rev-parse 94cd124b^{commit}
mkdir -p "$SRC"
git -c safe.directory='*' -C "$REPO" archive 94cd124b | tar -x -C "$SRC"
mkdir -p "$SRC/web/dist"
cp -a "$REPO/web/dist/." "$SRC/web/dist/"
( cd "$SRC/web/dist" && find . -type f ! -path './.vite/*' -print0 | sort -z | xargs -0 sha256sum ) > /var/tmp/cp-b6a-webdist.sha256
GO=/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go
export GOTOOLCHAIN=local CGO_ENABLED=0
$GO version
$GO env GOVERSION GOFLAGS GOPROXY GOMODCACHE
cd "$SRC"
mkdir -p "$A" "$T"
$GO build -trimpath -buildvcs=false -o "$A/agent" ./cmd/agent
$GO build -trimpath -buildvcs=false -tags dns_kill_matrix -o "$A/agent.kill" ./cmd/agent
$GO build -trimpath -buildvcs=false -o "$A/panel" ./cmd/panel
$GO build -trimpath -buildvcs=false -o "$A/dns-kill-trigger" ./cmd/dns-kill-matrix-trigger
$GO build -trimpath -buildvcs=false -o "$A/recovery" ./cmd/recovery
mapfile -t sources < deploy/recovery/agent-checker.sources
$GO build -trimpath -buildvcs=false -o "$A/agent-checker" "${sources[@]}"
mapfile -t sources < deploy/recovery/panel-checker.sources
$GO build -trimpath -buildvcs=false -o "$A/panel-checker" "${sources[@]}"
$GO build -trimpath -buildvcs=false -o "$A/schema17-bridge" ./deploy/schema17bridge
$GO run ./deploy/recovery/bundle --source-root . --binary-root "$A" --output "$A/recovery-runtime"
echo "== artifacts"
sha256sum "$A/agent" "$A/agent.kill" "$A/panel" "$A/dns-kill-trigger" "$A/recovery" "$A/agent-checker" "$A/panel-checker" "$A/schema17-bridge"
echo "== recovery-runtime"
( cd "$A/recovery-runtime" && find . -type f -print0 | sort -z | xargs -0 sha256sum )
cp /root/cp-b5-tools/smstatus.go "$T/smstatus.go"
printf '{"Replace":{"%s/cmd/oi-smstatus/main.go":"%s/smstatus.go"}}' "$SRC" "$T" > "$T/overlay.json"
$GO build -trimpath -buildvcs=false -overlay "$T/overlay.json" -o "$T/oi-smstatus" ./cmd/oi-smstatus
test ! -e "$SRC/cmd/oi-smstatus"
sha256sum "$T/oi-smstatus" "$T/overlay.json" "$T/smstatus.go"
$GO version -m "$A/agent" | head -3
echo "== comparison with the 65b86621 batch-5 build (/root/cp-b5-artifacts)"
for b in agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge recovery-runtime/runtime.manifest; do
  if cmp -s "$A/$b" "/root/cp-b5-artifacts/$b"; then echo "IDENTICAL $b"; else echo "DIFFERENT $b"; fi
done
diff <(cd /root/cp-b5-artifacts/recovery-runtime && find . -type f -print0 | sort -z | xargs -0 sha256sum) <(cd "$A/recovery-runtime" && find . -type f -print0 | sort -z | xargs -0 sha256sum) && echo "recovery-runtime tree IDENTICAL" || echo "recovery-runtime tree DIFFERENT"
cmp -s "$T/oi-smstatus" /root/cp-b5-tools/oi-smstatus && echo "IDENTICAL oi-smstatus" || echo "DIFFERENT oi-smstatus"
diff /var/tmp/cp-b5-webdist.sha256 /var/tmp/cp-b6a-webdist.sha256 && echo "web-dist IDENTICAL" || echo "web-dist DIFFERENT"
echo "== changed paths 65b86621..94cd124b (non-test Go and harness)"
git -c safe.directory='*' -C "$REPO" diff --stat 65b86621 94cd124b -- cmd internal deploy | tail -60
echo "== harness file hashes"
( cd "$SRC/deploy/e2e/dns-kill-matrix" && sha256sum fixture.py guest_bootstrap.py guest_bootstrap.sh run_cell.py guest_recovery_probe.py native_primary_peer.py native_primary_peer_probe.py manifest.json )
date -u +%FT%TZ
echo BUILD-OK
