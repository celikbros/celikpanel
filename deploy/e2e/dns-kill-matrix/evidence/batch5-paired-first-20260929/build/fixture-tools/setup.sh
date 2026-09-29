set -euo pipefail
exec > /var/tmp/cp-b5-setup.log 2>&1
cd /root/cp-b5-src
F=deploy/e2e/dns-kill-matrix/fixture.py
# Two work roots: the second run of the same cell id needs a fresh cell directory
# (cells/<sha256(cell_id)[:24]>), so c2/c4/c6 use the nested root r2.
for ROOT in /var/tmp/cp-b5-0929 /var/tmp/cp-b5-0929/r2; do
  echo "=== work root $ROOT"
  python3 $F init-root --work-root $ROOT --execute
  for i in Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2 debian-13-genericcloud-amd64-20260826-2582.qcow2; do
    ln /var/tmp/cp-v3n28/images/$i $ROOT/images/$i
  done
  ls -la $ROOT/images
  python3 $F verify-images --work-root $ROOT
  mkdir -m 0700 $ROOT/artifacts
  cp -a /root/cp-b5-artifacts/. $ROOT/artifacts/
  sha256sum $ROOT/artifacts/agent $ROOT/artifacts/agent.kill $ROOT/artifacts/panel $ROOT/artifacts/dns-kill-trigger $ROOT/artifacts/recovery
  sha256sum $ROOT/artifacts/recovery-runtime/runtime.manifest
done
ssh-keygen -q -t ed25519 -N '' -C cp-b5-0929 -f /var/tmp/cp-b5-0929/id_ed25519
cp -p /var/tmp/cp-b5-0929/id_ed25519 /var/tmp/cp-b5-0929/id_ed25519.pub /var/tmp/cp-b5-0929/r2/
sha256sum /var/tmp/cp-b5-0929/id_ed25519.pub /var/tmp/cp-b5-0929/r2/id_ed25519.pub
sha256sum deploy/e2e/dns-kill-matrix/manifest.json
ls -la /var/tmp/cp-b5-0929 /var/tmp/cp-b5-0929/r2
echo SETUP-OK
