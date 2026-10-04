set -euo pipefail
exec > /var/tmp/cp-b12-build.log 2>&1
date -u +%FT%TZ
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
C=2efc4de2
SRC=/root/cp-b12-src
A=/root/cp-b12-artifacts
T=/root/cp-b12-tools
test ! -e "$SRC"; test ! -e "$A"; test ! -e "$T"; test ! -e /var/tmp/cp-b12-1001
echo "repository HEAD at build time (record only): $(git -c safe.directory='*' -C "$REPO" rev-parse HEAD)"
echo "tested commit: $(git -c safe.directory='*' -C "$REPO" rev-parse $C^{commit})"
echo "branch accept/pdns-primary-gate-open-10: $(git -c safe.directory='*' -C "$REPO" rev-parse accept/pdns-primary-gate-open-10)"
echo "parent of tested commit: $(git -c safe.directory='*' -C "$REPO" rev-parse $C^)"
echo "main line d3d65353: $(git -c safe.directory='*' -C "$REPO" rev-parse d3d65353^{commit})"
mkdir -p "$SRC"
git -c safe.directory='*' -C "$REPO" archive $C | tar -x -C "$SRC"
test ! -e "$SRC/web/dist" && echo "web/dist absent in the archive (untracked build output)"
mkdir -p "$SRC/web/dist" && cp -a /root/cp-pair7/repo/web/dist/. "$SRC/web/dist/"
( cd "$SRC/web/dist" && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > /var/tmp/cp-b12-webdist.sha256
cmp /var/tmp/cp-b12-webdist.sha256 /root/cp-pair7/logs/web-dist-host.sha256 && echo "web/dist = the pair7 Windows npm run build of git archive 2efc4de2 web (106 files, identical hashes)"
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
mkdir -p "$A/dns-owner-tools"
for p in dns-peer-enroll bind-peer-inspect pdns-peer-inspect; do
  env -i HOME=/root PATH="$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 $GO build -trimpath -buildvcs=false -ldflags "-s -w" -o "$A/dns-owner-tools/$p" ./cmd/$p
done
cp cmd/dns-peer-enroll/README.md "$A/dns-owner-tools/README.md"
echo "== artifacts"
sha256sum "$A/agent" "$A/agent.kill" "$A/panel" "$A/dns-kill-trigger" "$A/recovery" "$A/agent-checker" "$A/panel-checker" "$A/schema17-bridge" "$A"/dns-owner-tools/*
echo "== recovery-runtime"
( cd "$A/recovery-runtime" && find . -type f -print0 | sort -z | xargs -0 sha256sum )
cp /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12/smstatus.go "$T/smstatus.go"
printf '{"Replace":{"%s/cmd/oi-smstatus/main.go":"%s/smstatus.go"}}' "$SRC" "$T" > "$T/overlay.json"
$GO build -trimpath -buildvcs=false -overlay "$T/overlay.json" -o "$T/oi-smstatus" ./cmd/oi-smstatus
test ! -e "$SRC/cmd/oi-smstatus"
sha256sum "$T/oi-smstatus" "$T/overlay.json" "$T/smstatus.go"
$GO version -m "$A/agent" | head -3
echo "== comparison with the 542ccc8e batch-11 build (/root/cp-b11-artifacts, read only)"
for b in agent agent.kill panel dns-kill-trigger recovery agent-checker panel-checker schema17-bridge recovery-runtime/runtime.manifest dns-owner-tools/dns-peer-enroll dns-owner-tools/bind-peer-inspect dns-owner-tools/pdns-peer-inspect; do
  if cmp -s "$A/$b" "/root/cp-b11-artifacts/$b"; then echo "IDENTICAL $b"; else echo "DIFFERENT $b"; fi
done
diff <(cd /root/cp-b11-artifacts/recovery-runtime && find . -type f -print0 | sort -z | xargs -0 sha256sum) <(cd "$A/recovery-runtime" && find . -type f -print0 | sort -z | xargs -0 sha256sum) && echo "recovery-runtime tree IDENTICAL" || echo "recovery-runtime tree DIFFERENT"
cmp -s "$T/oi-smstatus" /root/cp-b11-tools/oi-smstatus && echo "IDENTICAL oi-smstatus" || echo "DIFFERENT oi-smstatus"
echo "== gate constants in the tested source"
grep -n 'freshPairedPDNSPrimaryAdmitted\s*=' -r cmd/agent --include='*.go' | grep -v _test || true
grep -n 'freshPairedPDNSPrimaryOffered\s*=' -r cmd/panel --include='*.go' | grep -v _test || true
echo "== changed code paths 542ccc8e..$C (cmd/ internal/ deploy/e2e/dns-kill-matrix/*.py, excluding evidence)"
git -c safe.directory='*' -C "$REPO" diff --stat 542ccc8e $C -- cmd internal deploy/e2e/dns-kill-matrix/*.py deploy/e2e/dns-kill-matrix/*.sh deploy/e2e/dns-kill-matrix/manifest.json | tail -40
echo "== harness file hashes"
( cd "$SRC/deploy/e2e/dns-kill-matrix" && sha256sum fixture.py guest_bootstrap.py guest_bootstrap.sh run_cell.py guest_recovery_probe.py native_pdns_bind_peer.py native_pdns_peer_probe.py manifest.json )
echo "== offline harness tests (from the archive): all kill-matrix Python tests"
set +e
( cd "$SRC" && python3 -m unittest discover -s deploy/e2e/dns-kill-matrix -p 'test_*.py' 2>&1 | tail -6 ); echo "python unittest discover rc=${PIPESTATUS[0]}"
tot=0
for t in "$SRC"/deploy/e2e/dns-kill-matrix/test_*.py; do
  out=$( cd "$SRC" && python3 deploy/e2e/dns-kill-matrix/$(basename $t) 2>&1 | tail -3 )
  n=$(printf '%s\n' "$out" | grep -oE '^Ran [0-9]+' | grep -oE '[0-9]+'); ok=$(printf '%s\n' "$out" | grep -cE '^OK')
  echo "direct $(basename $t): ran=${n:-?} ok=$ok"; tot=$((tot + ${n:-0}))
done
echo "direct total ran=$tot"
echo "== offline trigger tests (go test -v ./cmd/dns-kill-matrix-trigger)"
( cd "$SRC" && $GO test -count=1 -v ./cmd/dns-kill-matrix-trigger > /var/tmp/cp-b12-triggertest.log 2>&1 ); echo "go test rc=$?"
echo "top-level PASS: $(grep -cE '^--- PASS' /var/tmp/cp-b12-triggertest.log) FAIL: $(grep -cE '^--- FAIL' /var/tmp/cp-b12-triggertest.log) SKIP: $(grep -cE '^--- SKIP' /var/tmp/cp-b12-triggertest.log)"
grep -E '^(=== RUN|--- (PASS|FAIL)).*TestFreshPrimaryZoneRecovery' /var/tmp/cp-b12-triggertest.log
tail -3 /var/tmp/cp-b12-triggertest.log
set -e
find "$SRC" -name __pycache__ -type d -prune -exec rm -rf {} +
date -u +%FT%TZ
echo BUILD-OK
