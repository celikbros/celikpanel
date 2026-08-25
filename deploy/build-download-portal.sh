#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' "Usage: $0 VERSION COMMIT PUBLISHED_AT ARCHIVE CHECKSUM OUTPUT_DIR" >&2
  exit 2
}

die() {
  printf '%s\n' "$1" >&2
  exit 1
}

valid_signed_release_version() {
  local value=$1
  local core prerelease major minor patch identifier
  [[ "$value" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)(-([0-9A-Za-z.-]+))?$ ]] || return 1
  major=${BASH_REMATCH[1]}
  minor=${BASH_REMATCH[2]}
  patch=${BASH_REMATCH[3]}
  prerelease=${BASH_REMATCH[5]:-}
  for core in "$major" "$minor" "$patch"; do
    [[ "$core" == 0 || "$core" != 0* ]] || return 1
  done
  if [[ -n "$prerelease" ]]; then
    [[ "$prerelease" != .* && "$prerelease" != *. &&
       "$prerelease" != *..* ]] || return 1
    IFS=. read -r -a identifiers <<< "$prerelease"
    for identifier in "${identifiers[@]}"; do
      [[ -n "$identifier" ]] || return 1
      if [[ "$identifier" =~ ^[0-9]+$ ]]; then
        [[ "$identifier" == 0 || "$identifier" != 0* ]] || return 1
      fi
    done
  fi
}

valid_release_sequence() {
  local value=$1 LC_ALL=C
  [[ "$value" =~ ^[1-9][0-9]*$ ]] || return 1
  (( ${#value} < 19 )) && return 0
  (( ${#value} == 19 )) || return 1
  [[ "$value" < 9223372036854775808 ]]
}

validate_pinned_regular_input() {
  local source=$1
  local descriptor_path=$2
  local label=$3
  local minimum_size=$4
  local maximum_size=$5
  local source_identity descriptor_identity links size
  [[ -f "$source" && ! -L "$source" ]] \
    || die "$label is not a regular non-symlink file"
  source_identity=$(stat -Lc '%d:%i' -- "$source") \
    || die "cannot inspect $label"
  descriptor_identity=$(stat -Lc '%d:%i' -- "$descriptor_path") \
    || die "cannot inspect opened $label"
  [[ "$source_identity" == "$descriptor_identity" ]] \
    || die "$label path changed while it was opened"
  links=$(stat -Lc '%h' -- "$descriptor_path")
  [[ "$links" == 1 ]] || die "$label must have exactly one hard link"
  size=$(stat -Lc '%s' -- "$descriptor_path")
  [[ "$size" =~ ^[0-9]+$ ]] || die "cannot determine $label size"
  (( 10#$size >= minimum_size && 10#$size <= maximum_size )) \
    || die "$label size is outside the accepted range"
}

verify_release_member_bytes() {
  local archive_file=$1
  local scratch=$2
  local member=$3
  local expected_file=$4
  local label=$5
  local count listing_file extracted_file
  [[ -f "$expected_file" && ! -L "$expected_file" ]] \
    || die "reviewed $label source is unavailable"
  count=$(grep -Fxc -- "$member" "$scratch/archive-members" || true)
  [[ "$count" == 1 ]] || die "release archive must contain exactly one $member"
  listing_file="$scratch/$label.listing"
  tar -tvzf "$archive_file" -- "$member" > "$listing_file" \
    || die "cannot inspect packaged $label"
  [[ "$(wc -l < "$listing_file" | tr -d '[:space:]')" == 1 &&
     "$(cut -c1 "$listing_file")" == - ]] \
    || die "packaged $label is not one regular archive member"
  extracted_file="$scratch/$label"
  tar -xOzf "$archive_file" -- "$member" > "$extracted_file" \
    || die "cannot read packaged $label"
  cmp -s -- "$expected_file" "$extracted_file" \
    || die "packaged $label differs from the reviewed source"
}

verify_release_provenance() {
  local archive_file=$1
  local scratch=$2
  local expected_version=$3
  local expected_commit=$4
  local expected_tree=$5
  local member label expected_file extracted_file listing_file count
  tar -tzf "$archive_file" > "$scratch/archive-members" \
    || die "cannot list the pre-signed release archive"
  for label in version commit tree; do
    member="celikpanel-$expected_version/release.$label"
    count=$(grep -Fxc -- "$member" "$scratch/archive-members" || true)
    [[ "$count" == 1 ]] \
      || die "release archive must contain exactly one $member"
    listing_file="$scratch/release.$label.listing"
    tar -tvzf "$archive_file" -- "$member" > "$listing_file" \
      || die "cannot inspect $member"
    [[ "$(wc -l < "$listing_file" | tr -d '[:space:]')" == 1 &&
       "$(cut -c1 "$listing_file")" == - ]] \
      || die "$member is not one regular archive member"
    extracted_file="$scratch/release.$label"
    tar -xOzf "$archive_file" -- "$member" > "$extracted_file" \
      || die "cannot read $member"
    expected_file="$scratch/expected-release.$label"
    case "$label" in
      version) printf '1\n' > "$expected_file" ;;
      commit) printf '%s\n' "$expected_commit" > "$expected_file" ;;
      tree) printf '%s\n' "$expected_tree" > "$expected_file" ;;
    esac
    cmp -s -- "$expected_file" "$extracted_file" \
      || die "$member does not match the approved release provenance"
  done
}

[[ $# -eq 6 ]] || usage
version=$1
commit=$2
published_at=$3
archive=$4
checksum=$5
output=$6

[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || {
  printf 'invalid release version: %s\n' "$version" >&2
  exit 1
}
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || {
  printf 'invalid release commit: %s\n' "$commit" >&2
  exit 1
}
[[ "$published_at" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || {
  printf 'invalid publication timestamp: %s\n' "$published_at" >&2
  exit 1
}
[[ -f "$archive" && ! -L "$archive" ]] || {
  printf 'archive is not a regular file: %s\n' "$archive" >&2
  exit 1
}
[[ -f "$checksum" && ! -L "$checksum" ]] || {
  printf 'checksum is not a regular file: %s\n' "$checksum" >&2
  exit 1
}
[[ ! -e "$output" && ! -L "$output" ]] || {
  printf 'output path already exists: %s\n' "$output" >&2
  exit 1
}

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd -- "$script_dir/.." && pwd)
template="$repo_root/download-portal"
[[ -d "$template" && ! -L "$template" ]] || {
  printf 'download portal template is unavailable\n' >&2
  exit 1
}
tracked_public_key="$repo_root/deploy/release-signing-ed25519.pem"
[[ -f "$tracked_public_key" && ! -L "$tracked_public_key" ]] || {
  printf 'tracked release-signing public key is unavailable\n' >&2
  exit 1
}
[[ "$(grep -Ec '^bootstrap_release_public_key_sha256=[0-9a-f]{64}$' "$template/get.sh")" -eq 1 ]] || {
  printf 'download bootstrap must contain one canonical public-key digest\n' >&2
  exit 1
}
bootstrap_public_key_sha256=$(sed -n 's/^bootstrap_release_public_key_sha256=//p' "$template/get.sh")
bootstrap_source_sha256=$(sha256sum "$template/get.sh" | awk '{print $1}')
[[ "$bootstrap_source_sha256" =~ ^[0-9a-f]{64}$ ]] \
  || die "cannot hash the download bootstrap"
[[ "$(sha256sum "$tracked_public_key" | awk '{print $1}')" == "$bootstrap_public_key_sha256" ]] || {
  printf 'tracked public key does not match the download bootstrap trust anchor\n' >&2
  exit 1
}
openssl pkey -pubin -passin pass: -in "$tracked_public_key" -pubout 2>/dev/null \
  | cmp -s - "$tracked_public_key" || {
    printf 'tracked release-signing public key is not canonical PEM\n' >&2
    exit 1
  }
openssl pkey -pubin -passin pass: -in "$tracked_public_key" -text -noout 2>/dev/null \
  | LC_ALL=C grep -Eq '^ED25519 Public-Key:' || {
    printf 'tracked release-signing public key is not Ed25519\n' >&2
    exit 1
  }
signed_manifest_writer="$repo_root/deploy/write-signed-release-manifest.sh"
signing_mode=${CELIKPANEL_RELEASE_SIGNING:-disabled}
signing_key=${CELIKPANEL_RELEASE_SIGNING_KEY_FILE:-}
platform_os=${CELIKPANEL_RELEASE_OS:-}
platform_arch=${CELIKPANEL_RELEASE_ARCH:-}
release_sequence=${CELIKPANEL_RELEASE_SEQUENCE:-}
release_tree=${CELIKPANEL_RELEASE_TREE:-}
signed_manifest_input=${CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE:-}
signed_signature_input=${CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE:-}
signed_mode=false
pre_signed=false
pre_signed_tmp=
cleanup() {
  [[ -z "$pre_signed_tmp" ]] || rm -rf -- "$pre_signed_tmp"
}
trap cleanup EXIT HUP INT TERM
case "$signing_mode" in
  disabled)
    [[ -z "$signing_key" && -z "$platform_os" && -z "$platform_arch" &&
       -z "$release_sequence" && -z "$release_tree" &&
       -z "$signed_manifest_input" && -z "$signed_signature_input" ]] || {
      printf 'partial release signing configuration is not allowed\n' >&2
      exit 1
    }
    ;;
  required)
    [[ -n "$signing_key" && -n "$platform_os" && -n "$platform_arch" &&
       -n "$release_sequence" ]] || {
      printf 'signed release mode requires key, sequence, OS, and architecture\n' >&2
      exit 1
    }
    [[ -z "$release_tree" && -z "$signed_manifest_input" &&
       -z "$signed_signature_input" ]] || {
      printf 'pre-signed release inputs are not allowed in required mode\n' >&2
      exit 1
    }
    [[ -f "$signed_manifest_writer" && ! -L "$signed_manifest_writer" ]] || {
      printf 'signed release manifest writer is unavailable\n' >&2
      exit 1
    }
    signed_mode=true
    ;;
  pre-signed)
    [[ -z "$signing_key" ]] || {
      printf 'pre-signed release mode refuses a private signing key\n' >&2
      exit 1
    }
    [[ -n "$platform_os" && -n "$platform_arch" &&
       -n "$release_sequence" && -n "$release_tree" &&
       -n "$signed_manifest_input" && -n "$signed_signature_input" ]] || {
      printf 'pre-signed release mode requires sequence, tree, OS, architecture, manifest, and signature\n' >&2
      exit 1
    }
    [[ "$release_tree" =~ ^[0-9a-f]{40}$ ]] || {
      printf 'invalid release tree: %s\n' "$release_tree" >&2
      exit 1
    }
    signed_mode=true
    pre_signed=true
    ;;
  *)
    printf 'invalid CELIKPANEL_RELEASE_SIGNING mode: %s\n' "$signing_mode" >&2
    exit 1
    ;;
esac

if [[ "$signed_mode" == true ]]; then
  valid_signed_release_version "$version" \
    || die "invalid signed release version: $version"
  valid_release_sequence "$release_sequence" \
    || die "invalid signed release sequence: $release_sequence"
  [[ "$platform_os" == linux ]] \
    || die "unsupported release operating system: $platform_os"
  [[ "$platform_arch" == amd64 || "$platform_arch" == arm64 ]] \
    || die "unsupported release architecture: $platform_arch"
  [[ "$(grep -Fxc "bootstrap_release_sequence=$release_sequence" "$template/get.sh")" -eq 1 &&
     "$(grep -Fxc "bootstrap_release_version=$version" "$template/get.sh")" -eq 1 ]] \
    || die "download bootstrap version/sequence does not match the signed release"
  expected_archive="celikpanel-$version-$platform_os-$platform_arch.tar.gz"
else
  expected_archive="celikpanel-$version.tar.gz"
fi
[[ "$(basename -- "$archive")" == "$expected_archive" ]] || {
  printf 'archive filename must be %s\n' "$expected_archive" >&2
  exit 1
}
[[ "$(basename -- "$checksum")" == "$expected_archive.sha256" ]] || {
  printf 'checksum filename must be %s.sha256\n' "$expected_archive" >&2
  exit 1
}

archive_source=$archive
checksum_source=$checksum
if [[ "$pre_signed" == true ]]; then
  expected_manifest_asset="celikpanel-$version-$platform_os-$platform_arch.release-manifest-v2"
  [[ "$(basename -- "$signed_manifest_input")" == "$expected_manifest_asset" ]] \
    || die "signed manifest filename must be $expected_manifest_asset"
  [[ "$(basename -- "$signed_signature_input")" == "$expected_manifest_asset.sig" ]] \
    || die "signed manifest signature filename must be $expected_manifest_asset.sig"
  exec {archive_fd}<"$archive"
  exec {checksum_fd}<"$checksum"
  exec {signed_manifest_fd}<"$signed_manifest_input"
  exec {signed_signature_fd}<"$signed_signature_input"
  archive_source=/proc/self/fd/$archive_fd
  checksum_source=/proc/self/fd/$checksum_fd
  signed_manifest_source=/proc/self/fd/$signed_manifest_fd
  signed_signature_source=/proc/self/fd/$signed_signature_fd
  validate_pinned_regular_input "$archive" "$archive_source" \
    "release archive" 1 2147483648
  validate_pinned_regular_input "$checksum" "$checksum_source" \
    "release checksum" 1 4096
  validate_pinned_regular_input "$signed_manifest_input" "$signed_manifest_source" \
    "signed release manifest" 1 4096
  validate_pinned_regular_input "$signed_signature_input" "$signed_signature_source" \
    "signed release manifest signature" 64 64
  pre_signed_tmp=$(mktemp -d)
fi

sha256=$(sha256sum "$archive_source" | awk '{print $1}')
[[ "$sha256" =~ ^[0-9a-f]{64}$ ]] || exit 1
if [[ "$pre_signed" == true ]]; then
  archive_size=$(stat -Lc '%s' -- "$archive_source")
  printf '%s  %s\n' "$sha256" "$expected_archive" \
    > "$pre_signed_tmp/expected-checksum"
  cmp -s -- "$pre_signed_tmp/expected-checksum" "$checksum_source" \
    || die "pre-signed release checksum bytes are not canonical"
  cat > "$pre_signed_tmp/expected-manifest" <<EOF
format=celikpanel-release-manifest-v2
sequence=$release_sequence
version=$version
commit=$commit
published_at=$published_at
os=$platform_os
arch=$platform_arch
archive=$expected_archive
archive_sha256=$sha256
archive_size=$archive_size
EOF
  cmp -s -- "$pre_signed_tmp/expected-manifest" "$signed_manifest_source" \
    || die "pre-signed release manifest does not match the approved release arguments"
  openssl pkeyutl -verify -rawin -pubin -inkey "$tracked_public_key" \
    -in "$signed_manifest_source" -sigfile "$signed_signature_source" \
    >/dev/null 2>&1 \
    || die "pre-signed release manifest signature does not verify"
  pre_archive_sha=$(sha256sum "$archive_source" | awk '{print $1}')
  pre_checksum_sha=$(sha256sum "$checksum_source" | awk '{print $1}')
  pre_manifest_sha=$(sha256sum "$signed_manifest_source" | awk '{print $1}')
  pre_signature_sha=$(sha256sum "$signed_signature_source" | awk '{print $1}')
  verify_release_provenance "$archive_source" "$pre_signed_tmp" \
    "$version" "$commit" "$release_tree"
  archive_root="celikpanel-$version"
  verify_release_member_bytes "$archive_source" "$pre_signed_tmp" \
    "$archive_root/libexec/get.sh" "$template/get.sh" packaged-get.sh
  verify_release_member_bytes "$archive_source" "$pre_signed_tmp" \
    "$archive_root/install.sh" "$repo_root/install.sh" packaged-install.sh
  verify_release_member_bytes "$archive_source" "$pre_signed_tmp" \
    "$archive_root/deploy/release-signing-ed25519.pem" \
    "$tracked_public_key" packaged-release-key.pem
else
  checksum_sha=$(awk -v name="$expected_archive" '
    NF == 2 && $2 == name && $1 ~ /^[0-9a-fA-F]{64}$/ { print tolower($1) }
  ' "$checksum_source")
  [[ "$checksum_sha" == "$sha256" ]] || {
    printf 'archive checksum does not match checksum file\n' >&2
    exit 1
  }
fi

umask 022
version_root="$output/releases/$version"
release_dir="$version_root"
if [[ "$signed_mode" == true ]]; then
  release_dir="$release_dir/$platform_os/$platform_arch"
fi
mkdir -p -- "$output/assets" "$release_dir" "$output/.well-known"
cp -- "$template/index.html" "$template/.htaccess" "$template/get.sh" "$output/"
cmp -s -- "$template/get.sh" "$output/get.sh" \
  || die "staged download bootstrap bytes differ"
[[ "$(sha256sum "$template/get.sh" | awk '{print $1}')" == "$bootstrap_source_sha256" &&
   "$(sha256sum "$output/get.sh" | awk '{print $1}')" == "$bootstrap_source_sha256" ]] \
  || die "download bootstrap changed during staging"
if [[ "$signed_mode" == true ]]; then
  [[ "$(grep -Fxc "bootstrap_release_sequence=$release_sequence" "$output/get.sh")" -eq 1 &&
     "$(grep -Fxc "bootstrap_release_version=$version" "$output/get.sh")" -eq 1 &&
     "$(grep -Fxc "bootstrap_release_public_key_sha256=$bootstrap_public_key_sha256" "$output/get.sh")" -eq 1 ]] \
    || die "staged download bootstrap trust pins changed"
fi
cp -- "$tracked_public_key" "$output/release-signing-ed25519.pem"
cmp -s -- "$tracked_public_key" "$output/release-signing-ed25519.pem" || {
  printf 'staged release-signing public key bytes differ\n' >&2
  exit 1
}
[[ "$(sha256sum "$output/release-signing-ed25519.pem" | awk '{print $1}')" == \
   "$bootstrap_public_key_sha256" ]] || {
  printf 'staged release-signing public key does not match the bootstrap trust anchor\n' >&2
  exit 1
}
cp -- "$template/assets/site.css" "$template/assets/site.js" "$output/assets/"
cp -- "$template/security.txt" "$output/.well-known/security.txt"
cp -- "$archive_source" "$release_dir/$expected_archive"
cp -- "$checksum_source" "$release_dir/$expected_archive.sha256"
# Re-prove exact bytes after staging. The signer hashes only this reviewed copy,
# never a pathname whose source/destination equality was merely assumed.
cmp -s -- "$archive_source" "$release_dir/$expected_archive" || {
  printf 'staged archive bytes differ from the source\n' >&2
  exit 1
}
[[ "$(sha256sum "$release_dir/$expected_archive" | awk '{print $1}')" == "$sha256" ]] || {
  printf 'staged archive digest differs from the verified source\n' >&2
  exit 1
}
cmp -s -- "$checksum_source" "$release_dir/$expected_archive.sha256" || {
  printf 'staged checksum bytes differ from the source\n' >&2
  exit 1
}
if [[ "$signing_mode" == required ]]; then
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE=$signing_key \
    bash "$signed_manifest_writer" \
      "$version" "$release_sequence" "$commit" "$published_at" \
      "$platform_os" "$platform_arch" \
      "$release_dir/$expected_archive" "$release_dir"
elif [[ "$pre_signed" == true ]]; then
  cp -- "$signed_manifest_source" "$release_dir/release-manifest-v2"
  cp -- "$signed_signature_source" "$release_dir/release-manifest-v2.sig"
  cmp -s -- "$signed_manifest_source" "$release_dir/release-manifest-v2" \
    || die "staged signed release manifest bytes differ"
  cmp -s -- "$signed_signature_source" "$release_dir/release-manifest-v2.sig" \
    || die "staged signed release signature bytes differ"
  [[ "$(sha256sum "$signed_manifest_source" | awk '{print $1}')" == "$pre_manifest_sha" &&
     "$(sha256sum "$release_dir/release-manifest-v2" | awk '{print $1}')" == "$pre_manifest_sha" ]] \
    || die "signed release manifest changed during staging"
  [[ "$(sha256sum "$signed_signature_source" | awk '{print $1}')" == "$pre_signature_sha" &&
     "$(sha256sum "$release_dir/release-manifest-v2.sig" | awk '{print $1}')" == "$pre_signature_sha" ]] \
    || die "signed release manifest signature changed during staging"
  [[ "$(sha256sum "$archive_source" | awk '{print $1}')" == "$pre_archive_sha" &&
     "$(sha256sum "$checksum_source" | awk '{print $1}')" == "$pre_checksum_sha" ]] \
    || die "pre-signed release inputs changed during staging"
  cmp -s -- "$pre_signed_tmp/expected-manifest" \
    "$release_dir/release-manifest-v2" \
    || die "staged signed manifest differs from the approved manifest"
  openssl pkeyutl -verify -rawin -pubin \
    -inkey "$output/release-signing-ed25519.pem" \
    -in "$release_dir/release-manifest-v2" \
    -sigfile "$release_dir/release-manifest-v2.sig" >/dev/null 2>&1 \
    || die "staged signed release manifest signature does not verify"
fi
if [[ "$signed_mode" == true ]]; then
  # Preserve the historical unsigned endpoint in the same portal tree. It is
  # a compatibility distribution copy, never an update authority.
  legacy_archive="celikpanel-$version.tar.gz"
  cp -- "$release_dir/$expected_archive" "$version_root/$legacy_archive"
  cmp -s -- "$release_dir/$expected_archive" "$version_root/$legacy_archive" || {
    printf 'legacy compatibility archive bytes differ from the signed archive\n' >&2
    exit 1
  }
  printf '%s  %s\n' "$sha256" "$legacy_archive" \
    > "$version_root/$legacy_archive.sha256"
fi
if [[ "$signed_mode" == true ]]; then
  archive_url="/releases/$version/$platform_os/$platform_arch/$expected_archive"
else
  archive_url="/releases/$version/$expected_archive"
fi
checksum_url="$archive_url.sha256"
if [[ "$signed_mode" == true ]]; then
manifest=$(cat <<JSON
{
  "version": "$version",
  "sequence": "$release_sequence",
  "commit": "$commit",
  "published_at": "$published_at",
  "os": "$platform_os",
  "arch": "$platform_arch",
  "sha256": "$sha256",
  "archive_url": "$archive_url",
  "checksum_url": "$checksum_url",
  "signed_manifest_url": "/releases/$version/$platform_os/$platform_arch/release-manifest-v2",
  "signed_manifest_signature_url": "/releases/$version/$platform_os/$platform_arch/release-manifest-v2.sig"
}
JSON
)
legacy_manifest=$(cat <<JSON
{
  "version": "$version",
  "commit": "$commit",
  "published_at": "$published_at",
  "sha256": "$sha256",
  "archive_url": "/releases/$version/celikpanel-$version.tar.gz",
  "checksum_url": "/releases/$version/celikpanel-$version.tar.gz.sha256"
}
JSON
)
else
manifest=$(cat <<JSON
{
  "version": "$version",
  "commit": "$commit",
  "published_at": "$published_at",
  "sha256": "$sha256",
  "archive_url": "$archive_url",
  "checksum_url": "$checksum_url"
}
JSON
)
fi
printf '%s\n' "$manifest" > "$release_dir/release.json"
if [[ "$signed_mode" == true ]]; then
  printf '%s\n' "$legacy_manifest" > "$version_root/release.json"
fi
printf '%s\n' "$manifest" > "$output/releases/latest.json"
printf '%s\n' "$version" > "$output/releases/latest.txt"
cat > "$output/releases/index.json" <<JSON
{
  "latest": "$version",
  "releases": [$manifest]
}
JSON

chmod 0755 "$output/get.sh"
find "$output" -type d -exec chmod 0755 {} +
find "$output" -type f ! -name get.sh -exec chmod 0644 {} +

printf 'Download portal built at %s\n' "$output"
printf 'Release %s SHA-256 %s\n' "$version" "$sha256"
