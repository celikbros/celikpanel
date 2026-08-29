#!/usr/bin/env bash
set -euo pipefail

build=/tmp/celikpanel-alpha35-portal-build-31c28e9-run32518010435
assets=/mnt/c/tmp/celikpanel-alpha35-release-assets-31c28e9-run32518010435
out=/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/portal-tree
package_a=/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/celikpanel-alpha35-portal-31c28e9-seq35-a.tar.gz
package_b=/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/celikpanel-alpha35-portal-31c28e9-seq35-b.tar.gz
package_final=/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/celikpanel-alpha35-portal-31c28e9-seq35.tar.gz
repo=/mnt/c/tmp/celikpanel-alpha35-portal-source-31c28e9
public_key=/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/release-public.pem
alpha34_assets=/mnt/c/tmp/celikpanel-alpha34-release-assets-c70e4c4

for path in "$build" "$out" "$package_a" "$package_b" "$package_final"; do
  test ! -e "$path"
done

# The clean PR-head worktree has tree
# 4a63878b9500a9db28c7581484be1788d03f692d, byte-identical to the peeled
# alpha.35 tag commit 31c28e941ddcde5cb0980ac471910bd98f6e1984. Pin every
# source byte consumed below independently of mutable worktree metadata.
source_check() {
  local name=$1 size=$2 digest=$3 path="$repo/$1"
  test -f "$path" && test ! -L "$path"
  test "$(stat -c %s -- "$path")" = "$size"
  test "$(sha256sum -- "$path" | awk '{print $1}')" = "$digest"
}
source_check deploy/build-download-portal.sh 6974 ed576fc470d1b9263e9d92931332fcce7ce9a840c08bb74cd67081caea5d25f5
source_check deploy/write-signed-release-manifest.sh 7839 2ba44a79622ef4dc699450b3818b8ab1baf975e90fee2e3d5b0e5348e58eb9fb
source_check download-portal/index.html 29042 ee8de2395a4d11647a201369c33b6fad6cff07029b139642075ff0007aa6efaf
source_check download-portal/.htaccess 851 465bfcb300eee6ad9687fb450cc48f9c90c6fc6edeced6276821f4f944ef20d6
source_check download-portal/get.sh 37513 13044fedc5826ec7282802f998508e7080f86837e906ae1bd611a9e40f2c1251
source_check download-portal/assets/site.css 38896 dc1e751bca2529538926217ceaeeb99bbd93bdbb3dfaa218e52ae1db6e32210b
source_check download-portal/assets/site.js 20724 9d9f499e952a0febe9da565d4762afed09f09d370b2b0b556e307857f920a792
source_check download-portal/security.txt 283 9e3262a7f9c084eb75e068a0ca83d86563d285bdbee395fdeeebf3dd4d661bee
test -f "$public_key" && test ! -L "$public_key"
test "$(sha256sum -- "$public_key" | awk '{print $1}')" = 7eadeb0b156f1a821575c4293fe664b44b8004bcdb5e9e770122cb5c144c68bb
test "$(find "$assets" -mindepth 1 -maxdepth 1 -type f | wc -l)" -eq 6
test "$(find "$assets" -mindepth 1 -maxdepth 1 ! -type f | wc -l)" -eq 0

check() {
  local name=$1 size=$2 digest=$3 path="$assets/$1"
  test -f "$path" && test ! -L "$path"
  test "$(stat -c %s -- "$path")" = "$size"
  test "$(sha256sum -- "$path" | awk '{print $1}')" = "$digest"
}

check celikpanel-v0.1.0-alpha.35.tar.gz 22255771 b588254f58bb6ade0adee22595c0cde1fa8119cfd55db615332bbdb50bc01a70
check celikpanel-v0.1.0-alpha.35.tar.gz.sha256 100 da641e9dda17b8456f339dd0fc4735b69ecf87fc23da426bf9797b199ae4f3de
check celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2 332 3240ffa25e0f34be323c74167bbe9022e565a0f7f4f7de55d164c3a1efc8db48
check celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2.sig 64 9755eaaf37e8944a07b8f98a4cc9f6ece9418e2c136e86728043f72128fecf2f
check celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz 22255771 b588254f58bb6ade0adee22595c0cde1fa8119cfd55db615332bbdb50bc01a70
check celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz.sha256 112 3f798069eab3ecf49fc989ebd225e95924de26f2b76fe9d78705eae52bd1015a

cmp -s -- \
  "$assets/celikpanel-v0.1.0-alpha.35.tar.gz" \
  "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz"
(cd "$assets" && sha256sum -c celikpanel-v0.1.0-alpha.35.tar.gz.sha256)
(cd "$assets" && sha256sum -c celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz.sha256)

# Anchor the local public key to the already-live alpha.34 signature before
# trusting it for alpha.35.
test "$(sha256sum -- "$alpha34_assets/celikpanel-v0.1.0-alpha.34-linux-amd64.release-manifest-v2" | awk '{print $1}')" = 0bb56c144bec93fa2b06d670e4e5df7db38b93a63a0171c93dd80e239dd09f91
test "$(sha256sum -- "$alpha34_assets/celikpanel-v0.1.0-alpha.34-linux-amd64.release-manifest-v2.sig" | awk '{print $1}')" = 333fd7f7ad36a53510e06147278cf0335528ed39eeebae52d4dd5cdd59de2591
openssl pkeyutl -verify -pubin \
  -inkey "$public_key" -rawin \
  -in "$alpha34_assets/celikpanel-v0.1.0-alpha.34-linux-amd64.release-manifest-v2" \
  -sigfile "$alpha34_assets/celikpanel-v0.1.0-alpha.34-linux-amd64.release-manifest-v2.sig" \
  >/dev/null
openssl pkeyutl -verify -pubin \
  -inkey "$public_key" -rawin \
  -in "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2" \
  -sigfile "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2.sig" \
  >/dev/null

umask 077
mkdir "$build"
openssl genpkey -algorithm ED25519 -out "$build/ephemeral-private.pem" >/dev/null 2>&1
CELIKPANEL_RELEASE_SIGNING=required \
CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$build/ephemeral-private.pem" \
CELIKPANEL_RELEASE_OS=linux \
CELIKPANEL_RELEASE_ARCH=amd64 \
CELIKPANEL_RELEASE_SEQUENCE=35 \
bash "$repo/deploy/build-download-portal.sh" \
  v0.1.0-alpha.35 \
  31c28e941ddcde5cb0980ac471910bd98f6e1984 \
  2026-08-21T19:05:46Z \
  "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz" \
  "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz.sha256" \
  "$build/portal"

platform="$build/portal/releases/v0.1.0-alpha.35/linux/amd64"
manifest="$platform/release-manifest-v2"
signature="$platform/release-manifest-v2.sig"
cmp -s -- "$manifest" "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2"
cp -- "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2" "$manifest"
cp -- "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.release-manifest-v2.sig" "$signature"
chmod 0644 "$manifest" "$signature"
rm -f -- "$build/ephemeral-private.pem"

openssl pkeyutl -verify -pubin \
  -inkey "$public_key" -rawin -in "$manifest" -sigfile "$signature" >/dev/null
cmp -s -- "$platform/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz" \
  "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz"
cmp -s -- "$platform/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz.sha256" \
  "$assets/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz.sha256"
cmp -s -- "$build/portal/releases/v0.1.0-alpha.35/celikpanel-v0.1.0-alpha.35.tar.gz" \
  "$assets/celikpanel-v0.1.0-alpha.35.tar.gz"
cmp -s -- "$build/portal/releases/v0.1.0-alpha.35/celikpanel-v0.1.0-alpha.35.tar.gz.sha256" \
  "$assets/celikpanel-v0.1.0-alpha.35.tar.gz.sha256"

cp -a -- "$build/portal" "$out"
cd "$build"
umask 022
tar --sort=name --format=gnu --owner=0 --group=0 --numeric-owner \
  --mtime=@1787339146 -cf - portal | gzip -n > "$package_a"
umask 077
tar --sort=name --format=gnu --owner=0 --group=0 --numeric-owner \
  --mtime=@1787339146 -cf - portal | gzip -n > "$package_b"
cmp -s -- "$package_a" "$package_b"
cp -- "$package_a" "$package_final"
sha256sum -- "$package_a" "$package_b" "$package_final"
stat -c '%n %s' -- "$package_a" "$package_b" "$package_final"
printf '%s\n' ALPHA35_PORTAL_TREE_AND_REPRODUCIBLE_PACKAGE_OK
