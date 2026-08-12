#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' "Usage: $0 VERSION COMMIT PUBLISHED_AT ARCHIVE CHECKSUM OUTPUT_DIR" >&2
  exit 2
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
signed_manifest_writer="$repo_root/deploy/write-signed-release-manifest.sh"
signing_mode=${CELIKPANEL_RELEASE_SIGNING:-disabled}
signing_key=${CELIKPANEL_RELEASE_SIGNING_KEY_FILE:-}
platform_os=${CELIKPANEL_RELEASE_OS:-}
platform_arch=${CELIKPANEL_RELEASE_ARCH:-}
release_sequence=${CELIKPANEL_RELEASE_SEQUENCE:-}
case "$signing_mode" in
  disabled)
    [[ -z "$signing_key" && -z "$platform_os" && -z "$platform_arch" &&
       -z "$release_sequence" ]] || {
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
    [[ -f "$signed_manifest_writer" && ! -L "$signed_manifest_writer" ]] || {
      printf 'signed release manifest writer is unavailable\n' >&2
      exit 1
    }
    ;;
  *)
    printf 'invalid CELIKPANEL_RELEASE_SIGNING mode: %s\n' "$signing_mode" >&2
    exit 1
    ;;
esac

if [[ "$signing_mode" == required ]]; then
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
sha256=$(sha256sum "$archive" | awk '{print $1}')
[[ "$sha256" =~ ^[0-9a-f]{64}$ ]] || exit 1
checksum_sha=$(awk -v name="$expected_archive" '
  NF == 2 && $2 == name && $1 ~ /^[0-9a-fA-F]{64}$/ { print tolower($1) }
' "$checksum")
[[ "$checksum_sha" == "$sha256" ]] || {
  printf 'archive checksum does not match checksum file\n' >&2
  exit 1
}

umask 022
version_root="$output/releases/$version"
release_dir="$version_root"
if [[ "$signing_mode" == required ]]; then
  release_dir="$release_dir/$platform_os/$platform_arch"
fi
mkdir -p -- "$output/assets" "$release_dir"
cp -- "$template/index.html" "$template/.htaccess" "$template/get.sh" "$output/"
cp -- "$template/assets/site.css" "$template/assets/site.js" "$output/assets/"
cp -- "$archive" "$checksum" "$release_dir/"
# Re-prove exact bytes after staging. The signer hashes only this reviewed copy,
# never a pathname whose source/destination equality was merely assumed.
cmp -s -- "$archive" "$release_dir/$expected_archive" || {
  printf 'staged archive bytes differ from the source\n' >&2
  exit 1
}
[[ "$(sha256sum "$release_dir/$expected_archive" | awk '{print $1}')" == "$sha256" ]] || {
  printf 'staged archive digest differs from the verified source\n' >&2
  exit 1
}
cmp -s -- "$checksum" "$release_dir/$expected_archive.sha256" || {
  printf 'staged checksum bytes differ from the source\n' >&2
  exit 1
}
if [[ "$signing_mode" == required ]]; then
  CELIKPANEL_RELEASE_SIGNING_KEY_FILE=$signing_key \
    bash "$signed_manifest_writer" \
      "$version" "$release_sequence" "$commit" "$published_at" \
      "$platform_os" "$platform_arch" \
      "$release_dir/$expected_archive" "$release_dir"
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

if [[ "$signing_mode" == required ]]; then
  archive_url="/releases/$version/$platform_os/$platform_arch/$expected_archive"
else
  archive_url="/releases/$version/$expected_archive"
fi
checksum_url="$archive_url.sha256"
if [[ "$signing_mode" == required ]]; then
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
if [[ "$signing_mode" == required ]]; then
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
