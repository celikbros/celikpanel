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

expected_archive="celikpanel-$version.tar.gz"
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

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd -- "$script_dir/.." && pwd)
template="$repo_root/download-portal"
[[ -d "$template" && ! -L "$template" ]] || {
  printf 'download portal template is unavailable\n' >&2
  exit 1
}

umask 022
mkdir -p -- "$output/assets" "$output/releases/$version"
cp -- "$template/index.html" "$template/.htaccess" "$template/get.sh" "$output/"
cp -- "$template/assets/site.css" "$template/assets/site.js" "$output/assets/"
cp -- "$archive" "$checksum" "$output/releases/$version/"

archive_url="/releases/$version/$expected_archive"
checksum_url="$archive_url.sha256"
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
printf '%s\n' "$manifest" > "$output/releases/$version/release.json"
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
