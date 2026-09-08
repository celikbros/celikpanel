#!/usr/bin/env bash
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
[[ $(id -u) == 0 ]] || { echo 'Run as root' >&2; exit 1; }
tmp=$(mktemp -d /var/lib/celikpanel-resume-test.XXXXXXXX)
trap '[[ "$tmp" == /var/lib/celikpanel-resume-test.* && ! -L "$tmp" ]] && rm -rf -- "$tmp"' EXIT
chmod 0700 "$tmp"
# Extract production functions; only the completion marker is isolated.
# Uretim fonksiyonlarini cikar; yalniz tamamlanma isaretini yalit.
awk '/^validate_root_directory_chain\(\)/ { capture=1 }
     /^# BEGIN DOWNLOAD OPERATION POLICY$/ { exit }
     capture { print }' "$repo/download-portal/get.sh" > "$tmp/policy.sh"
sed -i "s|/etc/celikpanel/install.complete|$tmp/complete|g" "$tmp/policy.sh"
openssl genpkey -algorithm ED25519 -out "$tmp/key" 2>/dev/null
openssl pkey -in "$tmp/key" -pubout -out "$tmp/public" 2>/dev/null
cat > "$tmp/probe.sh" <<'EOF'
set -eu
umask 077
test_root=$1
test_shell=$2
message() { printf '%s\n' "$1"; }
fail() { message "$1" >&2; exit 1; }
. "$test_root/policy.sh"
workdir=$test_root/work
state=$test_root/state
mkdir -m 0700 "$workdir" "$state"
release_sequence_floor=$state/sequence.floor
signed_update_lock=$state/update.lock
pending_install_directory=$state/install.pending
pending_install_identity=
resume_first_install=0
legacy_first_install=0
bootstrap_release_sequence=55
bootstrap_release_public_key_sha256=$(sha256sum "$test_root/public" | awk '{print $1}')
version=v0.1.0-alpha.55
signed_commit=0123456789abcdef0123456789abcdef01234567
signed_release_sequence=55
signed_archive_sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
signed_archive_size=1234
runtime_release_identity
signed_public_key_path=$workdir/release-signing-ed25519.pem
install -m 0600 "$test_root/public" "$signed_public_key_path"
printf '%s\n' format=celikpanel-release-manifest-v2 sequence=55 "version=$version" \
  "commit=$signed_commit" published_at=2026-09-08T15:02:51Z os=linux "arch=$runtime_release_arch" \
  "archive=celikpanel-$version-linux-$runtime_release_arch.tar.gz" \
  "archive_sha256=$signed_archive_sha256" "archive_size=$signed_archive_size" > "$workdir/release-manifest-v2"
openssl pkeyutl -sign -rawin -inkey "$test_root/key" -in "$workdir/release-manifest-v2" \
  -out "$workdir/release-manifest-v2.sig"
provision_first_install_signed_update_lock
acquire_signed_update_lock
publish_pending_first_install
initial=$(inspect_pending_first_install "$pending_install_directory")
# Publication is idempotent, and a competing process must not acquire FD 9.
# Yayim tekrarlanabilir; rakip surec FD 9 kilidini alamamali.
publish_pending_first_install
[ "$(inspect_pending_first_install "$pending_install_directory")" = "$initial" ]
if "$test_shell" -c 'exec 9>&-; . "$1"; signed_update_lock=$2; pending_first_install_is_idle' \
  test "$test_root/policy.sh" "$signed_update_lock"; then exit 1; fi
flock -u 9
exec 9>&-
pending_first_install_is_idle
reject() {
  if inspect_pending_first_install "$pending_install_directory" >/dev/null 2>&1; then
    echo "Accepted invalid receipt: $1" >&2; exit 1
  fi
}
chmod 0755 "$pending_install_directory"; reject directory-mode; chmod 0700 "$pending_install_directory"
chmod 0644 "$pending_install_directory/release-manifest-v2"; reject file-mode
chmod 0600 "$pending_install_directory/release-manifest-v2"
touch "$pending_install_directory/unexpected"; reject extra-entry; rm "$pending_install_directory/unexpected"
printf x >> "$pending_install_directory/release-manifest-v2.sig"; reject signature
install -m 0600 "$workdir/release-manifest-v2.sig" "$pending_install_directory/release-manifest-v2.sig"
mv "$pending_install_directory/release-manifest-v2" "$test_root/saved"
ln -s "$test_root/saved" "$pending_install_directory/release-manifest-v2"; reject symlink
rm "$pending_install_directory/release-manifest-v2"
mv "$test_root/saved" "$pending_install_directory/release-manifest-v2"
printf '%s\n' format=celikpanel-release-sequence-floor-v1 sequence=56 version=v0.1.0-alpha.56 > "$release_sequence_floor"
reject advanced-floor
rm "$release_sequence_floor"
bootstrap_release_sequence=54; reject future-receipt; bootstrap_release_sequence=56
[ "$(inspect_pending_first_install "$pending_install_directory")" = "$initial" ]
bootstrap_release_sequence=55
if finish_pending_first_install; then exit 1; fi
install -m 0600 /dev/null "$test_root/complete"
finish_pending_first_install
[ ! -e "$pending_install_directory" ]
printf 'First-install resume receipt contract passed (%s)\n' "$test_shell"
EOF
for policy_shell in bash dash; do
  "$policy_shell" "$tmp/probe.sh" "$tmp" "$policy_shell"
  rm -rf -- "$tmp/state" "$tmp/work"
  rm -f -- "$tmp/complete"
done
