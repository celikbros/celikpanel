#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
writer_source="$repo_root/deploy/write-signed-release-manifest.sh"
builder_source="$repo_root/deploy/build-download-portal.sh"
writer="$writer_source"
builder="$builder_source"
bootstrap="$repo_root/download-portal/get.sh"
installer="$repo_root/install.sh"
makefile="$repo_root/Makefile"
workflow="$repo_root/.github/workflows/ci.yml"

fail() {
  printf 'signed release manifest contract failed: %s\n' "$1" >&2
  exit 1
}
expect_rejected() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then
    fail "accepted $label"
  fi
}

command -v openssl >/dev/null 2>&1 || fail "openssl is required"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM

version=v0.1.0-alpha.44
sequence=44
commit=0123456789abcdef0123456789abcdef01234567
tree=89abcdef0123456789abcdef0123456789abcdef
published_at=2026-08-03T08:55:11Z
archive_name=celikpanel-$version-linux-amd64.tar.gz
archive="$tmp/$archive_name"
mkdir -p "$tmp/source/celikpanel-$version/bin"
cat > "$tmp/main.go" <<'EOF'
package main
func main() {}
EOF
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=local GOENV=off GOWORK=off \
  go build -trimpath -buildvcs=false \
    -o "$tmp/source/celikpanel-$version/bin/panel" "$tmp/main.go"
cp "$tmp/source/celikpanel-$version/bin/panel" \
  "$tmp/source/celikpanel-$version/bin/agent"
printf '1\n' > "$tmp/source/celikpanel-$version/release.version"
printf '%s\n' "$commit" > "$tmp/source/celikpanel-$version/release.commit"
printf '%s\n' "$tree" > "$tmp/source/celikpanel-$version/release.tree"

# These keys are ephemeral test fixtures, never release credentials.
openssl genpkey -algorithm ED25519 -out "$tmp/key.pem" >/dev/null 2>&1
openssl genpkey -algorithm ED25519 -out "$tmp/wrong-key.pem" >/dev/null 2>&1
chmod 0600 "$tmp/key.pem" "$tmp/wrong-key.pem"
openssl pkey -in "$tmp/key.pem" -passin pass: \
  -pubout -out "$tmp/public.pem" >/dev/null 2>&1
openssl pkey -in "$tmp/wrong-key.pem" -passin pass: \
  -pubout -out "$tmp/wrong-public.pem" >/dev/null 2>&1

test_repo="$tmp/test-repo"
mkdir -p "$test_repo/deploy" "$test_repo/download-portal"
cp -- "$writer_source" "$builder_source" "$test_repo/deploy/"
cp -a -- "$repo_root/download-portal/." "$test_repo/download-portal/"
cp -- "$installer" "$test_repo/install.sh"
cp -- "$tmp/public.pem" "$test_repo/deploy/release-signing-ed25519.pem"
test_public_key_sha256=$(sha256sum "$tmp/public.pem" | awk '{print $1}')
sed -i "s/^bootstrap_release_public_key_sha256=.*/bootstrap_release_public_key_sha256=$test_public_key_sha256/" \
  "$test_repo/download-portal/get.sh"
writer="$test_repo/deploy/write-signed-release-manifest.sh"
builder="$test_repo/deploy/build-download-portal.sh"

builder_sequence_probe="$tmp/builder-sequence-probe.sh"
{
  printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail'
  sed -n '/^valid_release_sequence() {$/,/^}$/p' "$builder"
  printf '%s\n' 'valid_release_sequence "$1"'
} > "$builder_sequence_probe"
chmod 0755 "$builder_sequence_probe"
for accepted_sequence in 1 9223372036854775807; do
  "$builder_sequence_probe" "$accepted_sequence" \
    || fail "portal builder rejected canonical sequence $accepted_sequence"
done
for rejected_sequence in 0 041 9223372036854775808 9999999999999999999; do
  expect_rejected "portal builder sequence $rejected_sequence" \
    "$builder_sequence_probe" "$rejected_sequence"
done

mkdir -p "$tmp/source/celikpanel-$version/libexec" \
  "$tmp/source/celikpanel-$version/deploy"
cp -- "$test_repo/download-portal/get.sh" \
  "$tmp/source/celikpanel-$version/libexec/get.sh"
cp -- "$test_repo/install.sh" \
  "$tmp/source/celikpanel-$version/install.sh"
cp -- "$tmp/public.pem" \
  "$tmp/source/celikpanel-$version/deploy/release-signing-ed25519.pem"
tar -czf "$archive" -C "$tmp/source" "celikpanel-$version"
(cd "$tmp" && sha256sum "$archive_name" > "$archive_name.sha256")
archive_sha=$(sha256sum "$archive" | awk '{print $1}')
archive_size=$(stat -Lc '%s' -- "$archive")

mkdir "$tmp/signed"
CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" linux amd64 \
    "$archive" "$tmp/signed"

cat > "$tmp/expected-manifest" <<EOF
format=celikpanel-release-manifest-v2
sequence=$sequence
version=$version
commit=$commit
published_at=$published_at
os=linux
arch=amd64
archive=$archive_name
archive_sha256=$archive_sha
archive_size=$archive_size
EOF
cmp "$tmp/expected-manifest" "$tmp/signed/release-manifest-v2" \
  || fail "manifest bytes are not canonical"
[[ "$(stat -Lc '%s' "$tmp/signed/release-manifest-v2.sig")" -eq 64 ]] \
  || fail "signature is not a raw Ed25519 signature"
openssl pkeyutl -verify -rawin -pubin -inkey "$tmp/public.pem" \
  -in "$tmp/signed/release-manifest-v2" \
  -sigfile "$tmp/signed/release-manifest-v2.sig" >/dev/null 2>&1 \
  || fail "writer output does not verify"

policy="$tmp/signed-policy.sh"
awk '
  /^# BEGIN SIGNED RELEASE MANIFEST POLICY$/ { capture = 1; next }
  /^# END SIGNED RELEASE MANIFEST POLICY$/ { capture = 0; next }
  capture { print }
' "$bootstrap" > "$policy"
grep -Fq 'verify_signed_release_manifest()' "$policy" \
  || fail "bootstrap verifier was not extracted"
bash -n "$policy" || fail "extracted verifier is not valid shell"
policy_probe="$tmp/policy-probe.sh"
cat > "$policy_probe" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
policy=$1
action=$2
shift 2
. "$policy"
case "$action" in
  key)
    probe_key=$1
    probe_uid=$2
    probe_gid=$3
    probe_mode=$4
    probe_links=$5
    probe_size=$6
    readlink() {
      printf '%s\n' "$probe_key"
    }
    dirname() {
      printf '%s\n' /trusted/root
    }
    validate_root_directory_chain() {
      [[ "$1" == /trusted/root ]]
    }
    validate_release_key_directory_chain() {
      [[ "$1" == /trusted/root ]]
    }
    stat() {
      printf '%s %s %s %s %s\n' \
        "$probe_uid" "$probe_gid" "$probe_mode" "$probe_links" "$probe_size"
    }
    validate_release_public_key "$probe_key"
    ;;
  runtime)
    probe_kernel=$1
    probe_machine=$2
    uname() {
      case "$1" in
        -s) printf '%s\n' "$probe_kernel" ;;
        -m) printf '%s\n' "$probe_machine" ;;
        *) return 1 ;;
      esac
    }
    runtime_release_identity
    printf '%s/%s\n' "$runtime_release_os" "$runtime_release_arch"
    ;;
  key-chain)
    direct_gid=$1
    direct_mode=$2
    upper_gid=$3
    upper_mode=$4
    readlink() { printf '%s\n' "$3"; }
    dirname() {
      case "$2" in
        /etc/celikpanel) printf '%s\n' /etc ;;
        /etc) printf '%s\n' / ;;
        *) return 1 ;;
      esac
    }
    stat() {
      case "${@: -1}" in
        /etc/celikpanel) printf '0 %s %s\n' "$direct_gid" "$direct_mode" ;;
        /etc) printf '0 %s %s\n' "$upper_gid" "$upper_mode" ;;
        /) printf '0 0 755\n' ;;
        *) return 1 ;;
      esac
    }
    validate_release_key_directory_chain /etc/celikpanel
    ;;
  key-directory-metadata)
    release_key_directory_metadata_allowed "$1" "$2" "$3" "$4"
    ;;
  *)
    exit 2
    ;;
esac
EOF
chmod 0755 "$policy_probe"
public_key_size=$(stat -Lc '%s' -- "$tmp/public.pem")
[[ "$("$policy_probe" "$policy" key "$tmp/public.pem" \
  0 0 644 1 "$public_key_size")" == "" ]] \
  || fail "safe Ed25519 public key probe emitted unexpected output"
expect_rejected "a non-root-owned release public key" "$policy_probe" \
  "$policy" key "$tmp/public.pem" 1000 0 644 1 "$public_key_size"
expect_rejected "a group-writable release public key" "$policy_probe" \
  "$policy" key "$tmp/public.pem" 0 0 664 1 "$public_key_size"
expect_rejected "a hard-linked release public key" "$policy_probe" \
  "$policy" key "$tmp/public.pem" 0 0 644 2 "$public_key_size"
expect_rejected "a private key installed as a release public key" "$policy_probe" \
  "$policy" key "$tmp/key.pem" 0 0 600 1 \
  "$(stat -Lc '%s' -- "$tmp/key.pem")"
ln -s "$tmp/public.pem" "$tmp/public-link.pem"
expect_rejected "a symlinked release public key" "$policy_probe" \
  "$policy" key "$tmp/public-link.pem" 0 0 644 1 "$public_key_size"
"$policy_probe" "$policy" key-directory-metadata 1 0 1234 750 \
  || fail "root:service-group mode-0750 direct key parent was rejected"
expect_rejected "a group-writable direct key parent" "$policy_probe" \
  "$policy" key-directory-metadata 1 0 1234 770
expect_rejected "a non-root-group upper key ancestor" "$policy_probe" \
  "$policy" key-directory-metadata 0 0 1234 755

[[ "$("$policy_probe" "$policy" runtime Linux x86_64)" == linux/amd64 ]] \
  || fail "x86_64 runtime did not map to linux/amd64"
[[ "$("$policy_probe" "$policy" runtime Linux aarch64)" == linux/arm64 ]] \
  || fail "aarch64 runtime did not map to linux/arm64"
expect_rejected "a FreeBSD signed-update runtime" "$policy_probe" \
  "$policy" runtime FreeBSD amd64
expect_rejected "an unsupported Linux architecture" "$policy_probe" \
  "$policy" runtime Linux s390x

version_probe="$tmp/version-probe.sh"
cat > "$version_probe" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
. "$1"
valid_release_version "$2"
EOF
chmod 0755 "$version_probe"
for accepted_version in v1.2.3 v1.2.3-alpha.9 v1.2.3-alpha.10 v0.0.0-0; do
  "$version_probe" "$policy" "$accepted_version" \
    || fail "canonical version was rejected: $accepted_version"
done
for rejected_version in v01.2.3 v1.02.3 v1.2.03 v1.2.3-01 \
  v1.2.3-.a v1.2.3-a..b v1.2.3+build; do
  expect_rejected "non-canonical version $rejected_version" \
    "$version_probe" "$policy" "$rejected_version"
done

mkdir "$tmp/max-sequence" "$tmp/invalid-sequence"
CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" 9223372036854775807 "$commit" "$published_at" \
    linux amd64 "$archive" "$tmp/max-sequence"
grep -Fxq 'sequence=9223372036854775807' \
  "$tmp/max-sequence/release-manifest-v2" \
  || fail "signer rejected or changed the INT64 maximum release sequence"
for invalid_sequence in 0 041 9223372036854775808 9999999999999999999; do
  expect_rejected "invalid signer sequence $invalid_sequence" env \
    CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
    bash "$writer" "$version" "$invalid_sequence" "$commit" "$published_at" \
      linux amd64 "$archive" "$tmp/invalid-sequence"
done

verify_shell="$tmp/verify.sh"
cat > "$verify_shell" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
policy=$1
shift
. "$policy"
verify_signed_release_manifest "$@"
EOF
chmod 0755 "$verify_shell"

verify_args=(
  "$policy" "$tmp/signed/release-manifest-v2"
  "$tmp/signed/release-manifest-v2.sig" "$tmp/public.pem"
  "$version" "$sequence" linux amd64 "$archive_name" "$commit" "$archive_sha" "$archive_size"
)
"$verify_shell" "${verify_args[@]}" \
  || fail "bootstrap rejected the canonical signed manifest"
"$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" "" "" "" \
  || fail "signed first-install verification rejected manifest-authorized identity fields"
expect_rejected "a wrong version" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" v9.9.9 "$sequence" linux amd64 "$archive_name" "$commit" "$archive_sha" "$archive_size"
expect_rejected "a wrong operating system" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" freebsd amd64 "$archive_name" "$commit" "$archive_sha" "$archive_size"
expect_rejected "a wrong architecture" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux arm64 "$archive_name" "$commit" "$archive_sha" "$archive_size"
expect_rejected "a wrong public key" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/wrong-public.pem" "$version" "$sequence" linux amd64 "$archive_name" "$commit" "$archive_sha" "$archive_size"
expect_rejected "a mismatched approved commit" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" \
  1111111111111111111111111111111111111111 "$archive_sha" "$archive_size"
expect_rejected "a mismatched approved archive digest" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" \
  "$commit" 1111111111111111111111111111111111111111111111111111111111111111 "$archive_size"
expect_rejected "a mismatched approved archive size" "$verify_shell" \
  "$policy" "$tmp/signed/release-manifest-v2" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" \
  "$commit" "$archive_sha" "$((archive_size + 1))"

cp "$tmp/signed/release-manifest-v2" "$tmp/tampered-manifest"
printf 'x' >> "$tmp/tampered-manifest"
expect_rejected "tampered signed bytes" "$verify_shell" \
  "$policy" "$tmp/tampered-manifest" "$tmp/signed/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" "$commit" "$archive_sha" "$archive_size"

cp "$tmp/signed/release-manifest-v2" "$tmp/noncanonical-manifest"
printf '\n' >> "$tmp/noncanonical-manifest"
openssl pkeyutl -sign -rawin -inkey "$tmp/key.pem" -passin pass: \
  -in "$tmp/noncanonical-manifest" -out "$tmp/noncanonical.sig"
expect_rejected "validly signed non-canonical metadata" "$verify_shell" \
  "$policy" "$tmp/noncanonical-manifest" "$tmp/noncanonical.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" "$commit" "$archive_sha" "$archive_size"

mkdir "$tmp/wrong-platform"
expect_rejected "a writer target for Windows" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" windows amd64 \
  "$archive" "$tmp/wrong-platform"
chmod 0644 "$tmp/key.pem"
mkdir "$tmp/loose-key"
expect_rejected "a signing key with loose permissions" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" linux amd64 \
  "$archive" "$tmp/loose-key"
chmod 0600 "$tmp/key.pem"

mkdir "$tmp/wrong-machine" "$tmp/wrong-machine-out"
wrong_machine_archive="$tmp/wrong-machine/celikpanel-$version-linux-arm64.tar.gz"
cp "$archive" "$wrong_machine_archive"
expect_rejected "an arm64 label on amd64 ELF binaries" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" linux arm64 \
  "$wrong_machine_archive" "$tmp/wrong-machine-out"

mkdir -p "$tmp/symlink-source/celikpanel-$version/bin" "$tmp/symlink-out"
ln -s /bin/false "$tmp/symlink-source/celikpanel-$version/bin/panel"
cp "$tmp/source/celikpanel-$version/bin/agent" \
  "$tmp/symlink-source/celikpanel-$version/bin/agent"
symlink_archive="$tmp/symlink-source/celikpanel-$version-linux-amd64.tar.gz"
tar -czf "$symlink_archive" -C "$tmp/symlink-source" "celikpanel-$version"
expect_rejected "a symbolic-link executable archive member" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" linux amd64 \
  "$symlink_archive" "$tmp/symlink-out"

mkdir -p "$tmp/hardlink-source/celikpanel-$version/bin" "$tmp/hardlink-out"
cp "$tmp/source/celikpanel-$version/bin/panel" \
  "$tmp/hardlink-source/celikpanel-$version/bin/panel"
ln "$tmp/hardlink-source/celikpanel-$version/bin/panel" \
  "$tmp/hardlink-source/celikpanel-$version/bin/agent"
hardlink_archive="$tmp/hardlink-source/celikpanel-$version-linux-amd64.tar.gz"
tar -czf "$hardlink_archive" -C "$tmp/hardlink-source" "celikpanel-$version"
expect_rejected "a hard-linked executable archive member" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" linux amd64 \
  "$hardlink_archive" "$tmp/hardlink-out"

mkdir "$tmp/oversize" "$tmp/oversize-out"
oversize_archive="$tmp/oversize/celikpanel-$version-linux-amd64.tar.gz"
truncate -s 2147483649 "$oversize_archive"
expect_rejected "an archive above the signed 2 GiB limit" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$version" "$sequence" "$commit" "$published_at" linux amd64 \
  "$oversize_archive" "$tmp/oversize-out"

invalid_version=v1.2.3-01
mkdir -p "$tmp/invalid-version/celikpanel-$invalid_version/bin" "$tmp/invalid-version-out"
cp "$tmp/source/celikpanel-$version/bin/panel" \
  "$tmp/invalid-version/celikpanel-$invalid_version/bin/panel"
cp "$tmp/source/celikpanel-$version/bin/agent" \
  "$tmp/invalid-version/celikpanel-$invalid_version/bin/agent"
invalid_version_archive="$tmp/invalid-version/celikpanel-$invalid_version-linux-amd64.tar.gz"
tar -czf "$invalid_version_archive" -C "$tmp/invalid-version" "celikpanel-$invalid_version"
expect_rejected "a non-canonical signer prerelease version" env \
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$writer" "$invalid_version" "$sequence" "$commit" "$published_at" linux amd64 \
  "$invalid_version_archive" "$tmp/invalid-version-out"

legacy_archive_name=celikpanel-$version.tar.gz
cp "$archive" "$tmp/$legacy_archive_name"
(cd "$tmp" && sha256sum "$legacy_archive_name" > "$legacy_archive_name.sha256")
bash "$builder" "$version" "$commit" "$published_at" \
  "$tmp/$legacy_archive_name" "$tmp/$legacy_archive_name.sha256" \
  "$tmp/legacy-site" >/dev/null
[[ ! -e "$tmp/legacy-site/releases/$version/release-manifest-v2" ]] \
  || fail "legacy portal build unexpectedly enabled signed updates"
[[ -f "$tmp/legacy-site/releases/$version/$legacy_archive_name" ]] \
  || fail "legacy unsigned versioned endpoint changed"

CELIKPANEL_RELEASE_SIGNING=required \
CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
CELIKPANEL_RELEASE_OS=linux \
CELIKPANEL_RELEASE_ARCH=amd64 \
CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/signed-site" >/dev/null
"$verify_shell" "$policy" \
  "$tmp/signed-site/releases/$version/linux/amd64/release-manifest-v2" \
  "$tmp/signed-site/releases/$version/linux/amd64/release-manifest-v2.sig" \
  "$tmp/public.pem" "$version" "$sequence" linux amd64 "$archive_name" \
  "$commit" "$archive_sha" "$archive_size" \
  || fail "signed portal build does not verify"
cmp "$archive" \
  "$tmp/signed-site/releases/$version/linux/amd64/$archive_name" \
  || fail "signed portal staged archive bytes changed"
cmp "$archive" \
  "$tmp/signed-site/releases/$version/$legacy_archive_name" \
  || fail "signed portal build did not preserve the legacy generic endpoint"
(cd "$tmp/signed-site/releases/$version" && \
  sha256sum -c "$legacy_archive_name.sha256" >/dev/null) \
  || fail "signed portal legacy compatibility checksum does not verify"
grep -Fq "/releases/$version/$legacy_archive_name" \
  "$tmp/signed-site/releases/$version/release.json" \
  || fail "signed portal build did not preserve legacy release.json semantics"
grep -Fq "\"sequence\": \"$sequence\"" "$tmp/signed-site/releases/latest.json" \
  || fail "signed portal schema does not transport sequence as a string"
grep -Fq "/releases/$version/linux/amd64/$archive_name" \
  "$tmp/signed-site/releases/latest.json" \
  || fail "signed portal schema does not use OS/arch-separated archive paths"
cmp "$tmp/public.pem" "$tmp/signed-site/release-signing-ed25519.pem" \
  || fail "signed portal root public key bytes changed"

official_manifest_name=celikpanel-$version-linux-amd64.release-manifest-v2
official_signature_name=$official_manifest_name.sig
official_manifest="$tmp/$official_manifest_name"
official_signature="$tmp/$official_signature_name"
cp -- "$tmp/signed/release-manifest-v2" "$official_manifest"
cp -- "$tmp/signed/release-manifest-v2.sig" "$official_signature"

# A portal publisher assembles the exact CI outputs. It must neither possess
# nor invoke the production manifest writer/private key.
mv -- "$writer" "$writer.unavailable"
env -u CELIKPANEL_RELEASE_SIGNING_KEY_FILE \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_TREE="$tree" \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$official_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$official_signature" \
    bash "$builder" "$version" "$commit" "$published_at" \
      "$archive" "$archive.sha256" "$tmp/pre-signed-site" >/dev/null
mv -- "$writer.unavailable" "$writer"

pre_signed_release_dir="$tmp/pre-signed-site/releases/$version/linux/amd64"
cmp "$official_manifest" "$pre_signed_release_dir/release-manifest-v2" \
  || fail "pre-signed portal changed official manifest bytes"
cmp "$official_signature" "$pre_signed_release_dir/release-manifest-v2.sig" \
  || fail "pre-signed portal changed official signature bytes"
cmp "$archive" "$pre_signed_release_dir/$archive_name" \
  || fail "pre-signed portal changed official archive bytes"
cmp "$archive.sha256" "$pre_signed_release_dir/$archive_name.sha256" \
  || fail "pre-signed portal changed official checksum bytes"
openssl pkeyutl -verify -rawin -pubin -inkey "$tmp/public.pem" \
  -in "$pre_signed_release_dir/release-manifest-v2" \
  -sigfile "$pre_signed_release_dir/release-manifest-v2.sig" >/dev/null 2>&1 \
  || fail "pre-signed portal manifest does not verify"
cmp "$tmp/public.pem" "$tmp/pre-signed-site/release-signing-ed25519.pem" \
  || fail "pre-signed portal root public key bytes changed"
cmp "$archive" \
  "$tmp/pre-signed-site/releases/$version/$legacy_archive_name" \
  || fail "pre-signed portal did not preserve the legacy generic endpoint"
grep -Fq "\"sequence\": \"$sequence\"" \
  "$tmp/pre-signed-site/releases/latest.json" \
  || fail "pre-signed portal schema does not transport sequence as a string"

printf '\n# local source mismatch probe\n' >> "$test_repo/install.sh"
expect_rejected "an archive containing a different installer than the reviewed source" env \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_TREE="$tree" \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$official_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$official_signature" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-wrong-installer"
cp -- "$installer" "$test_repo/install.sh"

pre_signed_env=(
  CELIKPANEL_RELEASE_SIGNING=pre-signed
  CELIKPANEL_RELEASE_OS=linux
  CELIKPANEL_RELEASE_ARCH=amd64
  CELIKPANEL_RELEASE_SEQUENCE=$sequence
  CELIKPANEL_RELEASE_TREE=$tree
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE=$official_manifest
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE=$official_signature
)
expect_rejected "a private key in pre-signed portal mode" env \
  "${pre_signed_env[@]}" CELIKPANEL_RELEASE_SIGNING_KEY_FILE="$tmp/key.pem" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-with-key"
expect_rejected "a missing release tree in pre-signed portal mode" env \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$official_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$official_signature" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-missing-tree"
expect_rejected "release arguments that differ from the signed manifest" env \
  "${pre_signed_env[@]}" \
  bash "$builder" "$version" "$commit" 2026-08-03T08:55:12Z \
    "$archive" "$archive.sha256" "$tmp/pre-signed-wrong-args"
expect_rejected "release-tree provenance that differs from the archive" env \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_TREE=1111111111111111111111111111111111111111 \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$official_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$official_signature" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-wrong-tree"

mkdir "$tmp/wrong-signature-assets"
wrong_signature="$tmp/wrong-signature-assets/$official_signature_name"
openssl pkeyutl -sign -rawin -inkey "$tmp/wrong-key.pem" -passin pass: \
  -in "$official_manifest" -out "$wrong_signature"
expect_rejected "a pre-signed manifest signed by another key" env \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_TREE="$tree" \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$official_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$wrong_signature" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-wrong-signature"

mkdir "$tmp/wrong-manifest-assets"
wrong_manifest="$tmp/wrong-manifest-assets/$official_manifest_name"
cp -- "$official_manifest" "$wrong_manifest"
printf 'x' >> "$wrong_manifest"
expect_rejected "tampered pre-signed manifest bytes" env \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_TREE="$tree" \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$wrong_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$official_signature" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-tampered-manifest"

mkdir "$tmp/noncanonical-checksum-assets"
noncanonical_checksum="$tmp/noncanonical-checksum-assets/$archive_name.sha256"
cp -- "$archive.sha256" "$noncanonical_checksum"
printf '\n' >> "$noncanonical_checksum"
expect_rejected "non-canonical pre-signed checksum bytes" env \
  "${pre_signed_env[@]}" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$noncanonical_checksum" "$tmp/pre-signed-bad-checksum"

mkdir "$tmp/wrong-name-assets"
wrong_name_manifest="$tmp/wrong-name-assets/release-manifest-v2"
cp -- "$official_manifest" "$wrong_name_manifest"
expect_rejected "a non-official pre-signed manifest filename" env \
  CELIKPANEL_RELEASE_SIGNING=pre-signed \
  CELIKPANEL_RELEASE_OS=linux \
  CELIKPANEL_RELEASE_ARCH=amd64 \
  CELIKPANEL_RELEASE_SEQUENCE="$sequence" \
  CELIKPANEL_RELEASE_TREE="$tree" \
  CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE="$wrong_name_manifest" \
  CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE="$official_signature" \
  bash "$builder" "$version" "$commit" "$published_at" \
    "$archive" "$archive.sha256" "$tmp/pre-signed-wrong-name"

expect_rejected "partial portal signing configuration" env \
  CELIKPANEL_RELEASE_SIGNING=required \
  bash "$builder" "$version" "$commit" "$published_at" \
  "$archive" "$archive.sha256" "$tmp/partial-site"

floor_probe="$tmp/floor-probe.sh"
cat > "$floor_probe" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
policy=$1
release_sequence_floor=$2
action=$3
expected_sequence=$4
minimum_sequence=$5
sequence=$6
version=$7
[[ "$minimum_sequence" != - ]] || minimum_sequence=
validate_root_directory_chain() { return 0; }
. "$policy"
case "$action" in
  enforce) enforce_release_sequence_floor "$sequence" "$version" ;;
  persist) persist_release_sequence_floor "$sequence" "$version" ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$floor_probe"
floor_state="$tmp/floor-state"
floor_file="$floor_state/sequence.floor"
mkdir -m 0700 "$floor_state"
expect_rejected "first enrollment without a trusted minimum" \
  "$floor_probe" "$policy" "$floor_file" enforce 42 - 42 "$version"
"$floor_probe" "$policy" "$floor_file" enforce 42 42 42 "$version" \
  || fail "trusted first-enrollment minimum was rejected"
"$floor_probe" "$policy" "$floor_file" persist 42 42 42 "$version" \
  || fail "initial rollback floor could not be persisted"
cat > "$tmp/expected-floor" <<EOF
format=celikpanel-release-sequence-floor-v1
sequence=42
version=$version
EOF
cmp "$tmp/expected-floor" "$floor_file" || fail "rollback floor bytes are not canonical"
[[ "$(stat -Lc '%u:%g:%a:%h' -- "$floor_file")" == 0:0:600:1 ]] \
  || fail "rollback floor metadata is unsafe"
"$floor_probe" "$policy" "$floor_file" enforce 42 - 42 "$version" \
  || fail "same-sequence same-version retry was rejected"
expect_rejected "same sequence with a different version" \
  "$floor_probe" "$policy" "$floor_file" enforce 42 - 42 v1.2.4
expect_rejected "a lower signed release sequence" \
  "$floor_probe" "$policy" "$floor_file" enforce 41 - 41 v1.2.2
"$floor_probe" "$policy" "$floor_file" enforce 43 - 43 v1.2.4 \
  || fail "a higher signed sequence was rejected"
expect_rejected "a sequence below the trusted worker minimum" \
  "$floor_probe" "$policy" "$floor_file" enforce 43 44 43 v1.2.4
"$floor_probe" "$policy" "$floor_file" persist 43 - 43 v1.2.4 \
  || fail "rollback floor could not advance"
grep -Fxq sequence=43 "$floor_file" || fail "rollback floor did not advance"

line_of() {
  grep -Fn -- "$1" "$bootstrap" | head -n 1 | cut -d: -f1
}
manifest_fetch_line=$(line_of 'signed_fetch "$release_url/release-manifest-v2"')
bootstrap_key_fetch_line=$(line_of 'signed_fetch "$base_url/release-signing-ed25519.pem"')
signed_lock_line=$(line_of 'acquire_signed_update_lock || fail \')
signature_verify_line=$(line_of 'verify_signed_release_manifest \')
archive_fetch_line=$(line_of 'signed_fetch "$release_url/$archive"')
signed_digest_line=$(line_of 'does not match the signed release manifest.')
archive_use_line=$(line_of 'tar -tzf "$workdir/$archive"')
signed_commit_line=$(line_of '[ "$extracted_release_commit" = "$signed_commit" ]')
floor_persist_line=$(line_of 'persist_release_sequence_floor "$signed_release_sequence" "$version"')
update_entry_line=$(line_of 'updater=$extracted_root/bootstrap-prebuilt-update.sh')
[[ "$bootstrap_key_fetch_line" -lt "$manifest_fetch_line" &&
   "$signed_lock_line" -lt "$manifest_fetch_line" &&
   "$manifest_fetch_line" -lt "$signature_verify_line" &&
   "$signature_verify_line" -lt "$archive_fetch_line" &&
   "$archive_fetch_line" -lt "$signed_digest_line" &&
   "$signed_digest_line" -lt "$archive_use_line" &&
   "$archive_use_line" -lt "$signed_commit_line" &&
   "$signed_commit_line" -lt "$floor_persist_line" &&
   "$floor_persist_line" -lt "$update_entry_line" ]] \
  || fail "bootstrap verification order is unsafe"
grep -Fq '[ "$requested_version" != latest ]' "$bootstrap" \
  || fail "signed updates do not require an exact version"
grep -Fq 'release_public_key=/etc/celikpanel/release-signing-ed25519.pem' "$bootstrap" \
  || fail "bootstrap does not pin the installed public-key path"
grep -Fxq 'bootstrap_release_sequence=44' "$bootstrap" \
  || fail "bootstrap does not pin the Alpha44 release sequence"
grep -Fxq 'bootstrap_release_version=v0.1.0-alpha.44' "$bootstrap" \
  || fail "bootstrap does not pin the Alpha44 release version"
grep -Fxq 'bootstrap_release_public_key_sha256=7eadeb0b156f1a821575c4293fe664b44b8004bcdb5e9e770122cb5c144c68bb' "$bootstrap" \
  || fail "bootstrap does not pin the reviewed public-key digest"
grep -Fq 'validate_bootstrap_release_public_key "$signed_public_key_path"' "$bootstrap" \
  || fail "bootstrap does not validate the downloaded public-key identity"
grep -Fq 'signed_update_lock=/var/lib/celikpanel-release-state/update.lock' "$bootstrap" \
  || fail "bootstrap does not pin the signed update lock path"
if awk '/acquire_signed_update_lock\(\)/,/^}/' "$bootstrap" \
    | grep -Eq '^[[:space:]]*(install|mktemp|mv)([[:space:]]|$)'; then
  fail "signed updater creates or replaces its own lock inode"
fi
grep -Fq "[ \"\$(stat -Lc '%u:%g:%a' -- \"\$signed_lock_directory\")\" = 0:0:700 ]" "$bootstrap" \
  || fail "signed updater does not require the exact release-state directory metadata"
grep -Fq "[ \"\$(stat -Lc '%u:%g:%a:%h:%s' -- /proc/self/fd/9)\" = 0:0:600:1:0 ]" "$bootstrap" \
  || fail "signed updater does not prove exact opened lock metadata"
grep -Fq '[ "$signed_lock_path_identity" = "$signed_lock_fd_identity" ]' "$bootstrap" \
  || fail "signed updater does not bind the lock pathname to the opened inode"
for exact_option in --expected-commit --expected-archive-sha256 --expected-archive-size; do
  grep -Fq -- "$exact_option" "$bootstrap" \
    || fail "bootstrap does not parse required exact-target option $exact_option"
done
grep -Fq '[ "$signed_manifest_commit" = "$signed_expected_commit" ]' "$bootstrap" \
  || fail "bootstrap does not bind the approved commit"
grep -Fq '[ "$signed_manifest_archive_sha256" = "$signed_expected_archive_sha256" ]' "$bootstrap" \
  || fail "bootstrap does not bind the approved archive digest"
grep -Fq '[ "$signed_manifest_archive_size" = "$signed_expected_archive_size" ]' "$bootstrap" \
  || fail "bootstrap does not bind the approved archive size"
grep -Fq 'for signed_required_command in openssl uname; do' "$bootstrap" \
  || fail "signed bootstrap runtime requirements are missing"
grep -Fq 'command -v flock >/dev/null 2>&1 || fail' "$bootstrap" \
  || fail "signed update lock runtime requirement is missing"
if grep -Fq 'tar tr uname xargs; do' "$bootstrap"; then
  fail "unsigned bootstrap flow unexpectedly requires uname"
fi
grep -Fq 'cp download-portal/get.sh dist/$(DIST)/libexec/get.sh' "$makefile" \
  || fail "reviewed updater is not packaged in release libexec"
grep -Fq 'RELEASE_UPDATER=/usr/libexec/celikpanel/get.sh' "$installer" \
  || fail "installer does not pin the reviewed updater target"
grep -Fq 'source=$SRC/libexec/get.sh' "$installer" \
  || fail "installer does not use the packaged updater source"
grep -Fq 'CELIKPANEL_RELEASE_PUBLIC_KEY_FILE' "$installer" \
  || fail "operator public-key provisioning input is missing"
grep -Fq 'provision_signed_update_lock' "$installer" \
  || fail "installer does not provision the signed update lock"
grep -Fq 'CELIKPANEL_FIRST_INSTALL_TRUST' "$installer" \
  || fail "installer has no all-or-none first-install trust contract"
grep -Fq 'enroll_first_install_signed_release_trust' "$installer" \
  || fail "installer does not enroll verified first-install trust"
grep -Fq 'deploy/enroll-signed-release-trust.sh' "$installer" \
  || fail "installer does not use the reviewed trust enrollment helper"
grep -Fq 'set -o noclobber; : > "$SIGNED_UPDATE_LOCK"' "$installer" \
  || fail "installer does not create the signed update lock atomically"
if awk '/provision_signed_update_lock\(\)/,/^}/' "$installer" \
    | grep -Eq '^[[:space:]]*(install|mktemp|mv|flock)([[:space:]]|$)'; then
  fail "installer can replace or recursively lock the signed update lock"
fi
grep -Fq 'RELEASE_STATE_DIR=/var/lib/celikpanel-release-state' "$installer" \
  || fail "installer does not pin the persistent release-state directory"
grep -Fq '[[ "$owner" == 0 && "$mode" == 600 && "$links" == 1 && "$size" == 0 &&' "$installer" \
  || fail "installer does not reject unsafe lock metadata"
grep -Fq '[[ "$path_identity" == "$fd_identity" ]]' "$installer" \
  || fail "installer does not prove stable lock/key descriptor identity"
grep -Fq 'validate_release_key_source_directory_chain "$(dirname -- "$key_source")"' "$installer" \
  || fail "operator public-key source ancestor validation is missing"
grep -Fq 'exec {key_source_fd}<"$key_source"' "$installer" \
  || fail "operator public-key source is not pinned to an open descriptor"
grep -Fq 'key_fd_path=/proc/self/fd/$key_source_fd' "$installer" \
  || fail "operator public-key descriptor path is missing"
grep -Fq 'installed release public key differs; automatic replacement is refused' "$installer" \
  || fail "installer can silently replace an enrolled release key"
grep -Fq 'cmp -s -- "$key_fd_path" "$RELEASE_PUBLIC_KEY"' "$installer" \
  || fail "installed public-key byte equality is not verified"
if grep -Eq 'genpkey|BEGIN PUBLIC KEY' "$installer"; then
  fail "installer generates or embeds release key material"
fi
grep -Fq 'secrets.CELIKPANEL_RELEASE_SIGNING_ED25519_PEM' "$workflow" \
  || fail "release CI has no explicit operator signing-key gate"
grep -Fq 'vars.CELIKPANEL_RELEASE_SEQUENCE' "$workflow" \
  || fail "release CI has no operator-controlled monotonic sequence gate"
grep -Fq 'CELIKPANEL_RELEASE_SIGNING_ED25519_PEM is required for tagged releases' "$workflow" \
  || fail "tagged releases do not require the signing key"
grep -Fq '[[ "$signed_count" -eq 4 ]]' "$workflow" \
  || fail "tagged releases do not require all signed assets"
grep -Fq 'release signing private key does not match the tracked public key' "$writer_source" \
  || fail "manifest writer does not bind the private key to the tracked public key"
grep -Fq 'cp -- "$tracked_public_key" "$output/release-signing-ed25519.pem"' "$builder_source" \
  || fail "portal builder does not publish the tracked public key at its root"
grep -Fq 'CELIKPANEL_RELEASE_SIGNING=pre-signed' "$repo_root/docs/release-signing.md" \
  || fail "pre-signed portal assembly is not documented"
grep -Fq 'pre-signed release mode refuses a private signing key' "$builder_source" \
  || fail "pre-signed portal mode does not explicitly refuse private keys"
signed_fetch_body=$(awk '
  /^signed_fetch\(\)/ { capture=1 }
  capture { print }
  capture && /^}/ { exit }
' "$bootstrap")
grep -Fq -- "--proto '=https'" <<< "$signed_fetch_body" \
  || fail "signed fetch does not require HTTPS"
grep -Fq -- '--max-filesize "$signed_fetch_limit"' <<< "$signed_fetch_body" \
  || fail "signed fetch does not enforce the authenticated byte limit"
if grep -Fq -- '--location' <<< "$signed_fetch_body"; then
  fail "signed fetch follows redirects"
fi
redirect_probe="$tmp/redirect-probe.sh"
cat > "$redirect_probe" <<EOF
#!/usr/bin/env bash
set -euo pipefail
$signed_fetch_body
curl() {
  local destination= previous= argument
  for argument in "\$@"; do
    if [[ "\$previous" == -o ]]; then destination=\$argument; fi
    previous=\$argument
  done
  printf 'redirect-body\n' > "\$destination"
  printf '302'
}
if signed_fetch https://celikpanel.net/releases/v1/linux/amd64/release-manifest-v2 \
    "$tmp/redirect-output" 4096; then
  exit 1
fi
[[ ! -e "$tmp/redirect-output" ]]
EOF
chmod 0755 "$redirect_probe"
"$redirect_probe" || fail "signed fetch accepted or retained a simulated 30x response"
require_size_line=$(line_of '[ "$signed_manifest_archive_size" -le 2147483648 ]')
[[ "$require_size_line" -lt "$archive_fetch_line" ]] \
  || fail "signed archive size is not bounded before download"
[[ "$(grep -Fc -- '-passin pass:' "$writer")" -eq 5 ]] \
  || fail "private-key OpenSSL calls are not explicitly non-interactive"
grep -Fq "go version -m" "$writer" \
  || fail "signer does not inspect actual Go GOOS/GOARCH metadata"

printf 'signed release manifest contract passed\n'
