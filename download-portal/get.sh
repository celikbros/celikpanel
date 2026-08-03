#!/bin/sh
set -eu

base_url=https://celikpanel.net
requested_version=latest

usage() {
  printf '%s\n' "Usage: $0 [--version vX.Y.Z[-prerelease]]"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      requested_version=$2
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

[ "$(id -u)" -eq 0 ] || {
  printf '%s\n' "CelikPanel installation must run as root." >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || {
  printf '%s\n' "curl is required. Install it with your operating system package manager." >&2
  exit 1
}
command -v sha256sum >/dev/null 2>&1 || {
  printf '%s\n' "sha256sum is required." >&2
  exit 1
}
command -v tar >/dev/null 2>&1 || {
  printf '%s\n' "tar is required." >&2
  exit 1
}

curl_fetch() {
  curl --fail --show-error --silent --location \
    --proto '=https' --tlsv1.2 --connect-timeout 20 --retry 3 \
    "$1" -o "$2"
}

workdir=$(mktemp -d "${TMPDIR:-/tmp}/celikpanel-install.XXXXXXXX")
case "$workdir" in
  "${TMPDIR:-/tmp}"/celikpanel-install.*) ;;
  *) printf '%s\n' "Unexpected temporary directory: $workdir" >&2; exit 1 ;;
esac
chmod 0700 "$workdir"
cleanup() { rm -rf -- "$workdir"; }
trap cleanup EXIT HUP INT TERM

if [ "$requested_version" = latest ]; then
  curl_fetch "$base_url/releases/latest.txt" "$workdir/latest.txt"
  version=$(tr -d '\r\n\t ' < "$workdir/latest.txt")
else
  version=$requested_version
fi

printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || {
  printf '%s\n' "Unsafe or invalid release version: $version" >&2
  exit 1
}

archive="celikpanel-$version.tar.gz"
release_url="$base_url/releases/$version"
curl_fetch "$release_url/$archive" "$workdir/$archive"
curl_fetch "$release_url/$archive.sha256" "$workdir/$archive.sha256"

expected_line=$(tr -d '\r' < "$workdir/$archive.sha256")
set -- $expected_line
[ "$#" -eq 2 ] || {
  printf '%s\n' "Checksum file has an unexpected format." >&2
  exit 1
}
checksum_value=$1
checksum_name=$2
[ "$checksum_name" = "$archive" ] && [ "${#checksum_value}" -eq 64 ] || {
  printf '%s\n' "Checksum file has an unexpected format." >&2
  exit 1
}
case "$checksum_value" in
  *[!0-9a-fA-F]*) printf '%s\n' "Checksum file has an unexpected format." >&2; exit 1 ;;
esac

(cd "$workdir" && sha256sum -c "$archive.sha256")

root="celikpanel-$version"
tar -tzf "$workdir/$archive" | awk -v root="$root" '
  BEGIN { ok = 1; count = 0 }
  {
    count++
    if ($0 ~ /^\// || $0 ~ /\\/ || $0 == ".." || $0 ~ /^\.\.\// || $0 ~ /\/\.\.($|\/)/) ok = 0
    if ($0 != root "/" && index($0, root "/") != 1) ok = 0
  }
  END { if (!ok || count == 0) exit 1 }
' || {
  printf '%s\n' "Archive contains unsafe or unexpected paths." >&2
  exit 1
}

mkdir "$workdir/extract"
tar -xzf "$workdir/$archive" -C "$workdir/extract" \
  --no-same-owner --no-same-permissions

installer="$workdir/extract/$root/install.sh"
[ -f "$installer" ] && [ ! -L "$installer" ] || {
  printf '%s\n' "The verified archive does not contain a regular install.sh." >&2
  exit 1
}

printf '%s\n' "Installing CelikPanel $version from verified archive $archive"
cd "$workdir/extract/$root"
/bin/sh "$installer"
