set -euo pipefail
exec > /var/tmp/cp-b9-build.log 2>&1
date -u +%FT%TZ
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
C=3cceb29a
SRC=/root/cp-b9-src
A=/root/cp-b9-artifacts
T=/root/cp-b9-tools
test ! -e "$SRC"; test ! -e "$A"; test ! -e "$T"; test ! -e /var/tmp/cp-b9-1001
echo "repository HEAD at build time (record only): $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD)"
echo "tested commit: $(git -c safe.directory='*' -C "$REPO" rev-parse $C^{commit})"
echo "branch accept/pdns-primary-gate-open-4: $(git -c safe.directory='*' -C "$REPO" rev-parse accept/pdns-primary-gate-open-4)"
echo "parent of tested commit: $(git -c safe.directory='*' -C "$REPO" rev-parse $C^)"
mkdir -p "$SRC"
git -c safe.directory='*' -C "$REPO" archive $C | tar -x -C "$SRC"
test ! -e "$SRC/web/dist" && echo "web/dist absent in the archive (untracked build output)"
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
# Owner tools (only used if the zone lifecycle needs the documented owner enrollment); Makefile dns-owner-tools flags
mkdir -p "$A/dns-owner-tools"
for p in dns-peer-enroll bind-peer-inspect pdns-peer-inspect; do
  env -i HOME=/root PATH="$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 $GO build -trimpath -buildvcs=false -ldflags "-s -w" -o "$A/dns-owner-tools/$p" ./cmd/$p
done
cp cmd/dns-peer-enroll/README.md "$A/dns-owner-tools/README.md"
echo "== artifacts"
sha256sum "$A/agent" "$A/agent.kill" "$A/panel" "$A/dns-kill-trigger" "$A/recovery" "$A/agent-checker" "$A/panel-checker" "$A/schema17-bridge" "$A"/dns-owner-tools/*
echo "== recovery-runtime"
( cd "$A/recovery-runtime" && find . -type f -print0 | sort -z | xargs -0 sha256sum )
cp /root/cp-b8r-tools/smstatus.go "$T/smstatus.go"
printf '{"Replace":{"%s/cmd/oi-smstatus/main.go":"%s/smstatus.go"}}' "$SRC" "$T" > "$T/overlay.json"
$GO build -trimpath -buildvcs=false -overlay "$T/overlay.json" -o "$T/oi-smstatus" ./cmd/oi-smstatus
test ! -e "$SRC/cmd/oi-smstatus"
sha256sum "$T/oi-smstatus" "$T/overlay.json" "$T/smstatus.go"
$GO version -m "$A/agent" | head -3
echo "== comparison with the e3591875 batch-8r build (/root/cp-b8r-artifacts, read only)"
for b in agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge recovery-runtime/runtime.manifest dns-owner-tools/dns-peer-enroll dns-owner-tools/bind-peer-inspect dns-owner-tools/pdns-peer-inspect; do
  if cmp -s "$A/$b" "/root/cp-b8r-artifacts/$b"; then echo "IDENTICAL $b"; else echo "DIFFERENT $b"; fi
done
diff <(cd /root/cp-b8r-artifacts/recovery-runtime && find . -type f -print0 | sort -z | xargs -0 sha256sum) <(cd "$A/recovery-runtime" && find . -type f -print0 | sort -z | xargs -0 sha256sum) && echo "recovery-runtime tree IDENTICAL" || echo "recovery-runtime tree DIFFERENT"
cmp -s "$T/oi-smstatus" /root/cp-b8r-tools/oi-smstatus && echo "IDENTICAL oi-smstatus" || echo "DIFFERENT oi-smstatus"
echo "== gate constants in the tested source"
grep -n 'freshPairedPDNSPrimaryAdmitted\s*=' -r cmd/agent --include='*.go' | grep -v _test || true
grep -n 'freshPairedPDNSPrimaryOffered\s*=' -r cmd/panel --include='*.go' | grep -v _test || true
echo "== changed paths e3591875..$C"
git -c safe.directory='*' -C "$REPO" diff --stat e3591875 $C | tail -40
echo "== harness file hashes"
( cd "$SRC/deploy/e2e/dns-kill-matrix" && sha256sum fixture.py guest_bootstrap.py guest_bootstrap.sh run_cell.py guest_recovery_probe.py native_pdns_bind_peer.py native_pdns_peer_probe.py manifest.json )
echo "== offline harness tests (from the archive): all kill-matrix Python tests"
set +e
( cd "$SRC" && python3 -m unittest discover -s deploy/e2e/dns-kill-matrix -p 'test_*.py' 2>&1 | tail -6 ); echo "python unittest discover rc=${PIPESTATUS[0]}"
for t in test_fresh_primary_v3.py test_fresh_primary_batch8_fixes.py test_fresh_primary_serial_zero_zone.py; do
  ( cd "$SRC" && python3 deploy/e2e/dns-kill-matrix/$t 2>&1 | tail -3 ); echo "$t done"
done
echo "== offline trigger tests (go test -v ./cmd/dns-kill-matrix-trigger)"
( cd "$SRC" && $GO test -count=1 -v ./cmd/dns-kill-matrix-trigger > /var/tmp/cp-b9-triggertest.log 2>&1 ); echo "go test rc=$?"
echo "top-level PASS: $(grep -cE '^--- PASS' /var/tmp/cp-b9-triggertest.log) FAIL: $(grep -cE '^--- FAIL' /var/tmp/cp-b9-triggertest.log) SKIP: $(grep -cE '^--- SKIP' /var/tmp/cp-b9-triggertest.log)"
tail -3 /var/tmp/cp-b9-triggertest.log
set -e
find "$SRC" -name __pycache__ -type d -prune -exec rm -rf {} +
date -u +%FT%TZ
echo BUILD-OK
