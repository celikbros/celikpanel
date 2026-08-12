#!/usr/bin/env bash
# Write and sign the exact, versioned release metadata consumed by the
# fail-closed updater path. The private key is operator/CI-owned and is never
# copied into the release tree.
set -euo pipefail

usage() {
  printf '%s\n' \
    "Usage: CELIKPANEL_RELEASE_SIGNING_KEY_FILE=PATH $0 VERSION SEQUENCE COMMIT PUBLISHED_AT OS ARCH ARCHIVE OUTPUT_DIR" >&2
  exit 2
}

die() {
  printf '%s\n' "$1" >&2
  exit 1
}

valid_release_version() {
  local value=$1 core prerelease identifier
  [[ "$value" != *+* ]] || return 1
  [[ "$value" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]] \
    || return 1
  prerelease=${value#*-}
  [[ "$prerelease" != "$value" ]] || return 0
  IFS=. read -r -a identifiers <<< "$prerelease"
  for identifier in "${identifiers[@]}"; do
    if [[ "$identifier" =~ ^[0-9]+$ ]]; then
      [[ "$identifier" == 0 || "$identifier" != 0* ]] || return 1
    fi
  done
}

[[ $# -eq 8 ]] || usage
version=$1
sequence=$2
commit=$3
published_at=$4
platform_os=$5
platform_arch=$6
archive=$7
output=$8
signing_key=${CELIKPANEL_RELEASE_SIGNING_KEY_FILE:-}

valid_release_version "$version" \
  || die "invalid release version: $version"
[[ "$sequence" =~ ^[1-9][0-9]*$ && ${#sequence} -le 19 ]] \
  || die "invalid release sequence: $sequence"
(( 10#$sequence <= 9223372036854775807 )) \
  || die "release sequence is outside the supported range"
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] \
  || die "invalid release commit: $commit"
[[ "$published_at" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] \
  || die "invalid publication timestamp: $published_at"
[[ "$platform_os" == linux ]] \
  || die "unsupported release operating system: $platform_os"
[[ "$platform_arch" == amd64 || "$platform_arch" == arm64 ]] \
  || die "unsupported release architecture: $platform_arch"
[[ -f "$archive" && ! -L "$archive" ]] \
  || die "archive is not a regular file: $archive"
[[ -d "$output" && ! -L "$output" ]] \
  || die "manifest output directory is unavailable: $output"
[[ -n "$signing_key" ]] \
  || die "CELIKPANEL_RELEASE_SIGNING_KEY_FILE is required"
[[ -f "$signing_key" && ! -L "$signing_key" ]] \
  || die "release signing key is not a regular file"
command -v openssl >/dev/null 2>&1 \
  || die "openssl is required to sign a release manifest"
command -v go >/dev/null 2>&1 \
  || die "go is required to verify release binary target metadata"

read -r key_owner key_mode key_links key_size \
  < <(stat -Lc '%u %a %h %s' -- "$signing_key")
[[ "$key_owner" -eq "$(id -u)" &&
   "$key_links" -eq 1 &&
   "$key_size" -ge 1 &&
   "$key_size" -le 16384 ]] \
  || die "release signing key ownership, link count, or size is unsafe"
key_permissions=$((0$key_mode))
[[ $((key_permissions & 0077)) -eq 0 ]] \
  || die "release signing key must not grant group or other permissions"
openssl pkey -in "$signing_key" -passin pass: -text -noout 2>/dev/null \
  | LC_ALL=C grep -Eq '^ED25519 Private-Key:' \
  || die "release signing key must be an unencrypted Ed25519 private key"

archive_name=$(basename -- "$archive")
expected_archive="celikpanel-$version-$platform_os-$platform_arch.tar.gz"
[[ "$archive_name" == "$expected_archive" ]] \
  || die "archive filename must be $expected_archive"
archive_size=$(stat -Lc '%s' -- "$archive")
[[ "$archive_size" =~ ^[1-9][0-9]*$ ]] \
  || die "release archive must not be empty"
(( 10#$archive_size <= 2147483648 )) \
  || die "release archive exceeds the signed-update size limit"
archive_sha256=$(sha256sum "$archive" | awk '{print $1}')
[[ "$archive_sha256" =~ ^[0-9a-f]{64}$ ]] \
  || die "invalid archive SHA-256"

# A filename label is not platform evidence. Inspect both shipped executables
# and bind the signature only to the actual little-endian ELF64 machine type.
archive_root="celikpanel-$version"
elf_tmp=$(mktemp -d)
cleanup_elf() { rm -rf -- "$elf_tmp"; }
trap cleanup_elf EXIT HUP INT TERM
for binary_name in panel agent; do
  member="$archive_root/bin/$binary_name"
  [[ "$(tar -tzf "$archive" | LC_ALL=C grep -Fxc -- "$member")" -eq 1 ]] \
    || die "archive must contain exactly one $member"
  member_record=$(LC_ALL=C tar -tvzf "$archive" -- "$member") \
    || die "cannot inspect archive metadata for $member"
  [[ "${member_record:0:1}" == - ]] \
    || die "$member must be a regular archive member"
  member_size=$(LC_ALL=C awk '{print $3}' <<< "$member_record")
  [[ "$member_size" =~ ^(0|[1-9][0-9]*)$ ]] \
    || die "$member has a non-canonical archive size"
  (( 10#$member_size >= 20 && 10#$member_size <= 268435456 )) \
    || die "$member size is outside the signed-release limit"
  tar -xOf "$archive" "$member" > "$elf_tmp/$binary_name" \
    || die "cannot inspect $member"
  [[ "$(stat -Lc '%s' -- "$elf_tmp/$binary_name")" == "$member_size" ]] \
    || die "$member extracted size differs from archive metadata"
  header=$(od -An -v -t x1 -N 20 -- "$elf_tmp/$binary_name" | tr -d ' \n')
  [[ "${header:0:16}" == 7f454c4602010100 ||
     "${header:0:16}" == 7f454c4602010103 ]] \
    || die "$member is not a little-endian ELF64 executable"
  machine=${header:36:4}
  case "$platform_arch:$machine" in
    amd64:3e00|arm64:b700) ;;
    *) die "$member ELF architecture does not match $platform_arch" ;;
  esac
  go_metadata=$(GOTOOLCHAIN=local GOENV=off GOWORK=off \
    go version -m "$elf_tmp/$binary_name" 2>/dev/null) \
    || die "$member has no readable Go build metadata"
  LC_ALL=C grep -Fqx $'\tbuild\tGOOS=linux' <<< "$go_metadata" \
    || die "$member Go build metadata does not target linux"
  LC_ALL=C grep -Fqx $'\tbuild\tGOARCH='"$platform_arch" <<< "$go_metadata" \
    || die "$member Go build metadata does not target $platform_arch"
done
rm -rf -- "$elf_tmp"
trap - EXIT HUP INT TERM

manifest=$output/release-manifest-v2
signature=$output/release-manifest-v2.sig
[[ ! -e "$manifest" && ! -L "$manifest" &&
   ! -e "$signature" && ! -L "$signature" ]] \
  || die "signed release manifest output already exists"

umask 077
manifest_tmp=$(mktemp "$output/.release-manifest-v2.XXXXXXXX")
signature_tmp=$(mktemp "$output/.release-manifest-v2.sig.XXXXXXXX")
public_key_tmp=$(mktemp "$output/.release-manifest-v2.pub.XXXXXXXX")
cleanup() {
  rm -f -- "$manifest_tmp" "$signature_tmp" "$public_key_tmp"
}
trap cleanup EXIT HUP INT TERM

cat > "$manifest_tmp" <<EOF
format=celikpanel-release-manifest-v2
sequence=$sequence
version=$version
commit=$commit
published_at=$published_at
os=$platform_os
arch=$platform_arch
archive=$archive_name
archive_sha256=$archive_sha256
archive_size=$archive_size
EOF

openssl pkeyutl -sign -rawin -inkey "$signing_key" -passin pass: \
  -in "$manifest_tmp" -out "$signature_tmp" >/dev/null 2>&1 \
  || die "failed to sign release manifest"
[[ "$(stat -Lc '%s' -- "$signature_tmp")" -eq 64 ]] \
  || die "Ed25519 release signature has an unexpected size"

# Self-check before either public file is moved into place.
openssl pkey -in "$signing_key" -passin pass: \
  -pubout -out "$public_key_tmp" >/dev/null 2>&1
openssl pkeyutl -verify -rawin -pubin -inkey "$public_key_tmp" \
  -in "$manifest_tmp" -sigfile "$signature_tmp" >/dev/null 2>&1 \
  || die "release manifest signature self-check failed"

# Fail if the source changed between digesting and signing. The portal builder
# performs a second equality proof after copying these bytes into staging.
[[ "$(stat -Lc '%s' -- "$archive")" == "$archive_size" &&
   "$(sha256sum "$archive" | awk '{print $1}')" == "$archive_sha256" ]] \
  || die "archive changed while the signed manifest was being created"

chmod 0644 "$manifest_tmp" "$signature_tmp"
mv -- "$signature_tmp" "$signature"
mv -- "$manifest_tmp" "$manifest"
trap - EXIT HUP INT TERM
rm -f -- "$public_key_tmp"

printf 'Signed release manifest written for %s %s/%s\n' \
  "$version" "$platform_os" "$platform_arch"
