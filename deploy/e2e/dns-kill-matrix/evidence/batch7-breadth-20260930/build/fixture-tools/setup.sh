set -euo pipefail
# usage: setup.sh ROOT  -- initialise one work root (the first call also creates the SSH key); appends to /var/tmp/cp-b7-setup.log
ROOT=$1
exec >> /var/tmp/cp-b7-setup.log 2>&1
cd /root/cp-b7-src
F=deploy/e2e/dns-kill-matrix/fixture.py
echo "=== $(date -u +%FT%TZ) work root $ROOT"
test ! -e $ROOT/images
python3 $F init-root --work-root $ROOT --execute
for i in Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2 debian-13-genericcloud-amd64-20260826-2582.qcow2; do
  ln /var/tmp/cp-v3n28/images/$i $ROOT/images/$i
done
ls -la $ROOT/images
python3 $F verify-images --work-root $ROOT
mkdir -m 0700 $ROOT/artifacts
cp -a /root/cp-b7-artifacts/. $ROOT/artifacts/
sha256sum $ROOT/artifacts/agent $ROOT/artifacts/agent.kill $ROOT/artifacts/panel $ROOT/artifacts/dns-kill-trigger $ROOT/artifacts/recovery
sha256sum $ROOT/artifacts/recovery-runtime/runtime.manifest
if [ ! -e /var/tmp/cp-b7-0930/id_ed25519 ]; then
  ssh-keygen -q -t ed25519 -N '' -C cp-b7-0930 -f /var/tmp/cp-b7-0930/id_ed25519
fi
if [ "$ROOT" != /var/tmp/cp-b7-0930 ]; then cp -p /var/tmp/cp-b7-0930/id_ed25519 /var/tmp/cp-b7-0930/id_ed25519.pub $ROOT/; fi
sha256sum $ROOT/id_ed25519.pub
sha256sum deploy/e2e/dns-kill-matrix/manifest.json
ls -la $ROOT
echo SETUP-OK $ROOT
