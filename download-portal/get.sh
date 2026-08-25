#!/bin/sh
set -eu
umask 077

base_url=https://celikpanel.net
bootstrap_release_sequence=42
bootstrap_release_version=v0.1.0-alpha.42
bootstrap_release_public_key_sha256=7eadeb0b156f1a821575c4293fe664b44b8004bcdb5e9e770122cb5c144c68bb
requested_version=latest
requested_action=auto
require_signed_manifest=0
bootstrap_signed_install=0
signed_release_mode=0
expected_sequence=
minimum_sequence=
expected_commit=
expected_archive_sha256=
expected_archive_size=
release_public_key=/etc/celikpanel/release-signing-ed25519.pem
release_sequence_floor=/var/lib/celikpanel-release-state/sequence.floor
signed_update_lock=/var/lib/celikpanel-release-state/update.lock
releases_root=/var/backups/celikpanel/releases
workdir=
signed_public_key_path=

message() {
  printf '%s / %s\n' "$1" "$2"
}

fail() {
  message "$1" "$2" >&2
  exit 1
}

usage() {
  message \
    "Usage: $0 [--version VERSION] [--install|--update] [--require-signed-manifest --expected-sequence N [--minimum-sequence N] --expected-commit COMMIT --expected-archive-sha256 SHA256 --expected-archive-size BYTES]" \
    "Kullanım: $0 [--version SÜRÜM] [--install|--update] [--require-signed-manifest --expected-sequence N [--minimum-sequence N] --expected-commit COMMIT --expected-archive-sha256 SHA256 --expected-archive-size BAYT]"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      requested_version=$2
      shift 2
      ;;
    --install|--update)
      [ "$requested_action" = auto ] || {
        message "Choose only one operation mode." "Yalnız bir işlem modu seçin." >&2
        exit 2
      }
      requested_action=${1#--}
      shift
      ;;
    --require-signed-manifest)
      require_signed_manifest=1
      shift
      ;;
    --expected-sequence)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      expected_sequence=$2
      shift 2
      ;;
    --minimum-sequence)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      minimum_sequence=$2
      shift 2
      ;;
    --expected-commit)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      expected_commit=$2
      shift 2
      ;;
    --expected-archive-sha256)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      expected_archive_sha256=$2
      shift 2
      ;;
    --expected-archive-size)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      expected_archive_size=$2
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

if [ "$require_signed_manifest" -eq 1 ]; then
  [ "$requested_action" = update ] || fail \
    "Signed-manifest mode is reserved for explicit updates." \
    "İmzalı-manifest modu yalnız açık güncellemeler içindir."
  [ "$requested_version" != latest ] || fail \
    "Signed-manifest updates require an exact release version." \
    "İmzalı-manifest güncellemeleri kesin bir sürüm gerektirir."
  [ -n "$expected_sequence" ] || fail \
    "Signed-manifest updates require an explicit expected release sequence." \
    "İmzalı güncellemeler açık bir beklenen sürüm sırası gerektirir."
else
  [ -z "$expected_sequence" ] && [ -z "$minimum_sequence" ] || fail \
    "Release sequence options require signed-manifest mode." \
    "Sürüm sırası seçenekleri imzalı-manifest modu gerektirir."
fi

[ "$(id -u)" -eq 0 ] || fail \
  "CelikPanel installation and updates must run as root." \
  "CelikPanel kurulumu ve güncellemeleri root olarak çalıştırılmalıdır."

if [ "$require_signed_manifest" -eq 1 ]; then
  [ -n "$expected_commit" ] && [ -n "$expected_archive_sha256" ] && [ -n "$expected_archive_size" ] || fail \
    "Signed-manifest updates require the exact approved commit, archive digest, and archive size." \
    "Signed update target identity is incomplete."
else
  [ -z "$expected_commit" ] && [ -z "$expected_archive_sha256" ] && [ -z "$expected_archive_size" ] || fail \
    "Release identity options require signed-manifest mode." \
    "Release identity options require signed-manifest mode."
fi

for required_command in awk bash chmod chown cmp curl dirname env find flock grep id install \
  mkdir mktemp mv od readlink rm sha256sum sort stat sync tar tr xargs; do
  command -v "$required_command" >/dev/null 2>&1 || fail \
    "$required_command is required. Install it with your operating system package manager." \
    "$required_command gereklidir. İşletim sisteminizin paket yöneticisiyle kurun."
done
for signed_required_command in openssl uname; do
  command -v "$signed_required_command" >/dev/null 2>&1 || fail \
    "$signed_required_command is required for signed release verification." \
    "İmzalı sürüm doğrulaması için $signed_required_command gereklidir."
done
if [ "$require_signed_manifest" -eq 1 ]; then
  command -v flock >/dev/null 2>&1 || fail \
    "flock is required for signed release verification." \
    "İmzalı sürüm doğrulaması için flock gereklidir."
fi

curl_fetch() {
  curl --fail --show-error --silent --location \
    --proto '=https' --tlsv1.2 --connect-timeout 20 --retry 3 \
    "$1" -o "$2"
}

# Signed assets are immutable exact-origin objects. Redirects are rejected;
# the legacy unsigned bootstrap retains its historical redirect behavior.
signed_fetch() {
  [ "$#" -eq 3 ] || return 2
  signed_fetch_limit=$3
  printf '%s\n' "$signed_fetch_limit" | LC_ALL=C grep -Eq '^[1-9][0-9]*$' || return 2
  [ "${#signed_fetch_limit}" -le 10 ] && [ "$signed_fetch_limit" -le 2147483648 ] || return 2
  signed_http_status=$(curl --fail --show-error --silent \
    --proto '=https' --tlsv1.2 --connect-timeout 20 --retry 3 \
    --max-filesize "$signed_fetch_limit" \
    --write-out '%{http_code}' "$1" -o "$2") || {
      rm -f -- "$2"
      return 1
    }
  [ "$signed_http_status" = 200 ] || {
    rm -f -- "$2"
    return 1
  }
}

cleanup() {
  [ -n "$workdir" ] || return 0
  case "$workdir" in
    /tmp/celikpanel-install.*|"$releases_root"/.download.*)
      rm -rf -- "$workdir"
      ;;
    *)
      message \
        "Refusing to clean an unexpected work directory: $workdir" \
        "Beklenmeyen çalışma dizini temizlenmiyor: $workdir" >&2
      ;;
  esac
}
trap cleanup EXIT HUP INT TERM

validate_root_directory_chain() {
  path=$1
  canonical=$(readlink -e -- "$path") || fail \
    "Trusted directory is unavailable: $path" \
    "Güvenilir dizin kullanılamıyor: $path"
  [ "$canonical" = "$path" ] || fail \
    "Trusted directory contains a symlink or alias: $path" \
    "Güvenilir dizin sembolik bağlantı veya takma yol içeriyor: $path"
  current=$path
  while :; do
    [ -d "$current" ] && [ ! -L "$current" ] || fail \
      "Unsafe trusted directory: $current" \
      "Güvenilir dizin güvenli değil: $current"
    set -- $(stat -Lc '%u %g %a' -- "$current")
    owner=$1
    group=$2
    mode=$3
    [ "$owner" -eq 0 ] && [ "$group" -eq 0 ] || fail \
      "Trusted directory must be owned by root: $current" \
      "Güvenilir dizin root sahipli olmalı: $current"
    permissions=$((0$mode))
    [ $((permissions & 0022)) -eq 0 ] || fail \
      "Trusted directory must not be group/other writable: $current" \
      "Güvenilir dizin grup/diğer kullanıcılarca yazılabilir olmamalı: $current"
    [ "$current" = / ] && break
    current=$(dirname -- "$current")
  done
}

# BEGIN SIGNED RELEASE MANIFEST POLICY
release_key_directory_metadata_allowed() {
  metadata_direct=$1
  metadata_owner=$2
  metadata_group=$3
  metadata_mode=$4
  [ "$metadata_owner" -eq 0 ] || return 1
  metadata_permissions=$((0$metadata_mode))
  [ $((metadata_permissions & 0022)) -eq 0 ] || return 1
  [ "$metadata_direct" -eq 1 ] || [ "$metadata_group" -eq 0 ]
}

validate_release_key_directory_chain() {
  key_directory=$1
  key_canonical=$(readlink -e -- "$key_directory") || return 1
  [ "$key_canonical" = "$key_directory" ] || return 1
  key_current=$key_directory
  key_direct_parent=1
  while :; do
    [ -d "$key_current" ] && [ ! -L "$key_current" ] || return 1
    set -- $(stat -Lc '%u %g %a' -- "$key_current") || return 1
    release_key_directory_metadata_allowed \
      "$key_direct_parent" "$1" "$2" "$3" || return 1
    [ "$key_current" = / ] && break
    key_current=$(dirname -- "$key_current")
    key_direct_parent=0
  done
}

acquire_signed_update_lock() {
  signed_lock_directory=$(dirname -- "$signed_update_lock")
  validate_root_directory_chain "$signed_lock_directory"
  [ "$(stat -Lc '%u:%g:%a' -- "$signed_lock_directory")" = 0:0:700 ] || return 1
  # Provisioning is installer-owned. Never create or replace this pathname
  # here: concurrent first-use creators could flock different inodes.
  [ -f "$signed_update_lock" ] && [ ! -L "$signed_update_lock" ] || return 1
  [ "$(readlink -e -- "$signed_update_lock")" = "$signed_update_lock" ] || return 1
  [ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$signed_update_lock")" = 0:0:600:1:0 ] || return 1
  exec 9<>"$signed_update_lock" || return 1
  signed_lock_path_identity=$(stat -Lc '%d:%i' -- "$signed_update_lock") || return 1
  signed_lock_fd_identity=$(stat -Lc '%d:%i' -- /proc/self/fd/9) || return 1
  [ "$signed_lock_path_identity" = "$signed_lock_fd_identity" ] || return 1
  flock -n 9 || return 1
  [ "$(stat -Lc '%d:%i' -- "$signed_update_lock")" = "$signed_lock_fd_identity" ] || return 1
  [ "$(stat -Lc '%u:%g:%a:%h:%s' -- /proc/self/fd/9)" = 0:0:600:1:0 ] || return 1
}

validate_release_public_key() {
  release_key_path=$1
  [ -f "$release_key_path" ] && [ ! -L "$release_key_path" ] || return 1
  release_key_canonical=$(readlink -e -- "$release_key_path") || return 1
  [ "$release_key_canonical" = "$release_key_path" ] || return 1
  validate_release_key_directory_chain "$(dirname -- "$release_key_path")"

  set -- $(stat -Lc '%u %g %a %h %s' -- "$release_key_path") || return 1
  [ "$1:$2:$4" = 0:0:1 ] || return 1
  release_key_permissions=$((0$3))
  [ $((release_key_permissions & 07133)) -eq 0 ] || return 1
  [ "$5" -ge 1 ] && [ "$5" -le 16384 ] || return 1

  openssl pkey -pubin -passin pass: -in "$release_key_path" \
    -pubout 2>/dev/null | cmp -s - "$release_key_path" || return 1
  openssl pkey -pubin -passin pass: -in "$release_key_path" \
    -text -noout 2>/dev/null \
    | LC_ALL=C grep -Eq '^ED25519 Public-Key:'
}

validate_bootstrap_release_public_key() {
  bootstrap_key_path=$1
  validate_release_public_key "$bootstrap_key_path" || return 1
  exec 8<"$bootstrap_key_path" || return 1
  bootstrap_key_path_identity=$(stat -Lc '%d:%i' -- "$bootstrap_key_path") || return 1
  bootstrap_key_fd_identity=$(stat -Lc '%d:%i' -- /proc/self/fd/8) || return 1
  [ "$bootstrap_key_path_identity" = "$bootstrap_key_fd_identity" ] || return 1
  [ "$(stat -Lc '%u:%g:%a:%h' -- /proc/self/fd/8)" = 0:0:600:1 ] || return 1
  bootstrap_key_fd_sha256=$(sha256sum /proc/self/fd/8 | awk '{print $1}') || return 1
  [ "$bootstrap_key_fd_sha256" = "$bootstrap_release_public_key_sha256" ] || return 1
  openssl pkey -pubin -passin pass: -in /proc/self/fd/8 -pubout 2>/dev/null |
    cmp -s - /proc/self/fd/8 || return 1
  [ "$(stat -Lc '%d:%i' -- "$bootstrap_key_path")" = "$bootstrap_key_fd_identity" ] || return 1
  exec 8<&-
}

runtime_release_identity() {
  runtime_release_os=
  runtime_release_arch=
  [ "$(uname -s 2>/dev/null)" = Linux ] || return 1
  case "$(uname -m 2>/dev/null)" in
    x86_64)
      runtime_release_arch=amd64
      ;;
    aarch64|arm64)
      runtime_release_arch=arm64
      ;;
    *)
      return 1
      ;;
  esac
  runtime_release_os=linux
}

valid_release_version() {
  release_version_value=$1
  case "$release_version_value" in *+*) return 1 ;; esac
  printf '%s\n' "$release_version_value" | LC_ALL=C grep -Eq \
    '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$' \
    || return 1
  case "$release_version_value" in
    *-*) release_prerelease=${release_version_value#*-} ;;
    *) return 0 ;;
  esac
  release_old_ifs=$IFS
  IFS=.
  set -- $release_prerelease
  IFS=$release_old_ifs
  for release_identifier do
    if printf '%s\n' "$release_identifier" | LC_ALL=C grep -Eq '^[0-9]+$'; then
      [ "$release_identifier" = 0 ] || case "$release_identifier" in 0*) return 1 ;; esac
    fi
  done
}

decimal_not_greater_than() {
  decimal_left=$1
  decimal_right=$2
  [ "${#decimal_left}" -eq "${#decimal_right}" ] || return 1
  while [ -n "$decimal_left" ]; do
    decimal_left_tail=${decimal_left#?}
    decimal_right_tail=${decimal_right#?}
    decimal_left_digit=${decimal_left%"$decimal_left_tail"}
    decimal_right_digit=${decimal_right%"$decimal_right_tail"}
    [ "$decimal_left_digit" -lt "$decimal_right_digit" ] && return 0
    [ "$decimal_left_digit" -gt "$decimal_right_digit" ] && return 1
    decimal_left=$decimal_left_tail
    decimal_right=$decimal_right_tail
  done
  return 0
}

valid_release_sequence() {
  release_sequence_value=$1
  printf '%s\n' "$release_sequence_value" | LC_ALL=C grep -Eq '^[1-9][0-9]*$' \
    || return 1
  [ "${#release_sequence_value}" -le 19 ] || return 1
  [ "${#release_sequence_value}" -lt 19 ] || \
    decimal_not_greater_than "$release_sequence_value" 9223372036854775807
}

inspect_release_sequence_floor() {
  floor_present=0
  floor_sequence=
  floor_version=
  floor_state_dir=$(dirname -- "$release_sequence_floor")
  validate_root_directory_chain /var/lib
  if [ ! -e "$floor_state_dir" ] && [ ! -L "$floor_state_dir" ]; then
    return 0
  fi
  validate_root_directory_chain "$floor_state_dir"
  [ "$(stat -Lc '%u:%g:%a' -- "$floor_state_dir")" = 0:0:700 ] || return 1
  if [ ! -e "$release_sequence_floor" ] && [ ! -L "$release_sequence_floor" ]; then
    return 0
  fi
  [ -f "$release_sequence_floor" ] && [ ! -L "$release_sequence_floor" ] || return 1
  [ "$(readlink -e -- "$release_sequence_floor")" = "$release_sequence_floor" ] || return 1
  set -- $(stat -Lc '%u %g %a %h %s' -- "$release_sequence_floor") || return 1
  [ "$1:$2:$3:$4" = 0:0:600:1 ] && [ "$5" -ge 1 ] && [ "$5" -le 512 ] || return 1
  floor_format=
  floor_sequence_line=
  floor_version_line=
  floor_extra=
  {
    IFS= read -r floor_format || return 1
    IFS= read -r floor_sequence_line || return 1
    IFS= read -r floor_version_line || return 1
    if IFS= read -r floor_extra || [ -n "$floor_extra" ]; then return 1; fi
  } < "$release_sequence_floor"
  [ "$floor_format" = format=celikpanel-release-sequence-floor-v1 ] || return 1
  floor_sequence=${floor_sequence_line#sequence=}
  floor_version=${floor_version_line#version=}
  [ "$floor_sequence_line" = "sequence=$floor_sequence" ] || return 1
  [ "$floor_version_line" = "version=$floor_version" ] || return 1
  valid_release_sequence "$floor_sequence" && valid_release_version "$floor_version" || return 1
  printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
    "sequence=$floor_sequence" "version=$floor_version" \
    | cmp -s -- "$release_sequence_floor" - || return 1
  floor_present=1
}

preflight_first_install_trust_state() {
  preflight_key_source=$1
  preflight_sequence=$2
  preflight_version=$3
  preflight_key_directory=$(dirname -- "$release_public_key")
  validate_root_directory_chain "$(dirname -- "$preflight_key_directory")"
  if [ -e "$preflight_key_directory" ] || [ -L "$preflight_key_directory" ]; then
    validate_release_key_directory_chain "$preflight_key_directory" || return 1
  elif [ -e "$release_public_key" ] || [ -L "$release_public_key" ]; then
    return 1
  fi
  if [ -e "$release_public_key" ] || [ -L "$release_public_key" ]; then
    validate_release_public_key "$release_public_key" || return 1
    [ "$(stat -Lc '%u:%g:%a:%h' -- "$release_public_key")" = 0:0:644:1 ] || return 1
    cmp -s -- "$preflight_key_source" "$release_public_key" || return 1
  fi

  inspect_release_sequence_floor || return 1
  if [ "$floor_present" -eq 1 ]; then
    [ "$floor_sequence" = "$preflight_sequence" ] &&
      [ "$floor_version" = "$preflight_version" ] || return 1
  fi
  preflight_state_dir=$(dirname -- "$release_sequence_floor")
  if [ -e "$preflight_state_dir" ] || [ -L "$preflight_state_dir" ]; then
    if [ -e "$signed_update_lock" ] || [ -L "$signed_update_lock" ]; then
      [ -f "$signed_update_lock" ] && [ ! -L "$signed_update_lock" ] || return 1
      [ "$(readlink -e -- "$signed_update_lock")" = "$signed_update_lock" ] || return 1
      [ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$signed_update_lock")" = 0:0:600:1:0 ] || return 1
    fi
  elif [ -e "$signed_update_lock" ] || [ -L "$signed_update_lock" ]; then
    return 1
  fi
}

provision_first_install_signed_update_lock() {
  first_lock_directory=$(dirname -- "$signed_update_lock")
  first_lock_parent=$(dirname -- "$first_lock_directory")
  first_state_created=0
  first_lock_created=0
  validate_root_directory_chain "$first_lock_parent"
  if [ ! -e "$first_lock_directory" ] && [ ! -L "$first_lock_directory" ]; then
    if mkdir -m 0700 -- "$first_lock_directory" 2>/dev/null; then
      first_state_created=1
    elif [ ! -d "$first_lock_directory" ] || [ -L "$first_lock_directory" ]; then
      return 1
    fi
  fi
  validate_root_directory_chain "$first_lock_directory"
  [ "$(stat -Lc '%u:%g:%a' -- "$first_lock_directory")" = 0:0:700 ] || return 1
  if [ "$first_state_created" -eq 1 ]; then
    sync -f -- "$first_lock_directory" "$first_lock_parent" || return 1
  fi

  if [ ! -e "$signed_update_lock" ] && [ ! -L "$signed_update_lock" ]; then
    if (umask 077; set -C; : > "$signed_update_lock") 2>/dev/null; then
      first_lock_created=1
    elif [ ! -e "$signed_update_lock" ] && [ ! -L "$signed_update_lock" ]; then
      return 1
    fi
  fi
  [ -f "$signed_update_lock" ] && [ ! -L "$signed_update_lock" ] || return 1
  [ "$(readlink -e -- "$signed_update_lock")" = "$signed_update_lock" ] || return 1
  if [ "$first_lock_created" -eq 1 ]; then
    chown root:root -- "$signed_update_lock" && chmod 0600 -- "$signed_update_lock" ||
      return 1
    sync -f -- "$signed_update_lock" "$first_lock_directory" || return 1
  fi
  [ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$signed_update_lock")" = 0:0:600:1:0 ]
}

enforce_release_sequence_floor() {
  enforce_sequence=$1
  enforce_version=$2
  valid_release_sequence "$expected_sequence" || return 1
  [ "$enforce_sequence" = "$expected_sequence" ] || return 1
  if [ -n "$minimum_sequence" ]; then
    valid_release_sequence "$minimum_sequence" || return 1
    [ "$enforce_sequence" -ge "$minimum_sequence" ] || return 1
  fi
  inspect_release_sequence_floor || return 1
  [ "$floor_present" -eq 1 ] || [ -n "$minimum_sequence" ] || return 1
  if [ "$floor_present" -eq 1 ]; then
    [ "$enforce_sequence" -ge "$floor_sequence" ] || return 1
    [ "$enforce_sequence" -ne "$floor_sequence" ] || \
      [ "$enforce_version" = "$floor_version" ] || return 1
  fi
}

persist_release_sequence_floor() {
  persist_sequence=$1
  persist_version=$2
  inspect_release_sequence_floor || return 1
  if [ "$floor_present" -eq 1 ] && [ "$persist_sequence" -eq "$floor_sequence" ]; then
    [ "$persist_version" = "$floor_version" ]
    return
  fi
  floor_state_dir=$(dirname -- "$release_sequence_floor")
  if [ ! -e "$floor_state_dir" ] && [ ! -L "$floor_state_dir" ]; then
    install -d -m 0700 -o root -g root -- "$floor_state_dir" || return 1
    sync -f -- /var/lib || return 1
  fi
  validate_root_directory_chain "$floor_state_dir"
  [ "$(stat -Lc '%u:%g:%a' -- "$floor_state_dir")" = 0:0:700 ] || return 1
  floor_tmp=$(mktemp "$floor_state_dir/.sequence.floor.XXXXXXXX") || return 1
  if ! printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
      "sequence=$persist_sequence" "version=$persist_version" > "$floor_tmp" ||
     ! chown root:root -- "$floor_tmp" || ! chmod 0600 -- "$floor_tmp" ||
     ! sync -f -- "$floor_tmp" || ! mv -T -- "$floor_tmp" "$release_sequence_floor" ||
     ! sync -f -- "$floor_state_dir"; then
    rm -f -- "$floor_tmp"
    return 1
  fi
  inspect_release_sequence_floor && [ "$floor_present" -eq 1 ] &&
    [ "$floor_sequence" = "$persist_sequence" ] && [ "$floor_version" = "$persist_version" ]
}

verify_signed_release_manifest() {
  [ "$#" -eq 11 ] || return 1
  signed_manifest_path=$1
  signed_signature_path=$2
  signed_public_key_path=$3
  signed_expected_version=$4
  signed_expected_sequence=$5
  signed_expected_os=$6
  signed_expected_arch=$7
  signed_expected_archive=$8
  signed_expected_commit=$9
  signed_expected_archive_sha256=${10}
  signed_expected_archive_size=${11}
  signed_release_sequence=
  signed_commit=
  signed_archive_sha256=
  signed_archive_size=

  [ -f "$signed_manifest_path" ] && [ ! -L "$signed_manifest_path" ] || return 1
  [ -f "$signed_signature_path" ] && [ ! -L "$signed_signature_path" ] || return 1
  set -- $(stat -Lc '%s %h' -- "$signed_manifest_path") || return 1
  [ "$1" -ge 1 ] && [ "$1" -le 4096 ] && [ "$2" -eq 1 ] || return 1
  set -- $(stat -Lc '%s %h' -- "$signed_signature_path") || return 1
  [ "$1:$2" = 64:1 ] || return 1

  openssl pkeyutl -verify -rawin -pubin -passin pass: \
    -inkey "$signed_public_key_path" -in "$signed_manifest_path" \
    -sigfile "$signed_signature_path" >/dev/null 2>&1 || return 1

  signed_line_format=
  signed_line_sequence=
  signed_line_version=
  signed_line_commit=
  signed_line_published_at=
  signed_line_os=
  signed_line_arch=
  signed_line_archive=
  signed_line_archive_sha256=
  signed_line_archive_size=
  signed_line_extra=
  {
    IFS= read -r signed_line_format || return 1
    IFS= read -r signed_line_sequence || return 1
    IFS= read -r signed_line_version || return 1
    IFS= read -r signed_line_commit || return 1
    IFS= read -r signed_line_published_at || return 1
    IFS= read -r signed_line_os || return 1
    IFS= read -r signed_line_arch || return 1
    IFS= read -r signed_line_archive || return 1
    IFS= read -r signed_line_archive_sha256 || return 1
    IFS= read -r signed_line_archive_size || return 1
    if IFS= read -r signed_line_extra || [ -n "$signed_line_extra" ]; then
      return 1
    fi
  } < "$signed_manifest_path"

  [ "$signed_line_format" = format=celikpanel-release-manifest-v2 ] || return 1
  signed_sequence=${signed_line_sequence#sequence=}
  signed_version=${signed_line_version#version=}
  signed_manifest_commit=${signed_line_commit#commit=}
  signed_published_at=${signed_line_published_at#published_at=}
  signed_os=${signed_line_os#os=}
  signed_arch=${signed_line_arch#arch=}
  signed_archive=${signed_line_archive#archive=}
  signed_manifest_archive_sha256=${signed_line_archive_sha256#archive_sha256=}
  signed_manifest_archive_size=${signed_line_archive_size#archive_size=}
  [ "$signed_line_sequence" = "sequence=$signed_sequence" ] || return 1
  [ "$signed_line_version" = "version=$signed_version" ] || return 1
  [ "$signed_line_commit" = "commit=$signed_manifest_commit" ] || return 1
  [ "$signed_line_published_at" = "published_at=$signed_published_at" ] || return 1
  [ "$signed_line_os" = "os=$signed_os" ] || return 1
  [ "$signed_line_arch" = "arch=$signed_arch" ] || return 1
  [ "$signed_line_archive" = "archive=$signed_archive" ] || return 1
  [ "$signed_line_archive_sha256" = "archive_sha256=$signed_manifest_archive_sha256" ] || return 1
  [ "$signed_line_archive_size" = "archive_size=$signed_manifest_archive_size" ] || return 1

  valid_release_sequence "$signed_sequence" || return 1
  valid_release_version "$signed_version" || return 1
  printf '%s\n' "$signed_manifest_commit" | LC_ALL=C grep -Eq '^[0-9a-f]{40}$' || return 1
  printf '%s\n' "$signed_published_at" | LC_ALL=C grep -Eq \
    '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' || return 1
  [ "$signed_os" = linux ] || return 1
  case "$signed_arch" in amd64|arm64) ;; *) return 1 ;; esac
  [ "$signed_archive" = "celikpanel-$signed_version-$signed_os-$signed_arch.tar.gz" ] || return 1
  printf '%s\n' "$signed_manifest_archive_sha256" | LC_ALL=C grep -Eq \
    '^[0-9a-f]{64}$' || return 1
  printf '%s\n' "$signed_manifest_archive_size" | LC_ALL=C grep -Eq \
    '^[1-9][0-9]*$' || return 1
  [ "${#signed_manifest_archive_size}" -le 10 ] &&
    [ "$signed_manifest_archive_size" -le 2147483648 ] || return 1
  [ "$signed_version" = "$signed_expected_version" ] || return 1
  [ "$signed_sequence" = "$signed_expected_sequence" ] || return 1
  [ "$signed_os" = "$signed_expected_os" ] || return 1
  [ "$signed_arch" = "$signed_expected_arch" ] || return 1
  [ "$signed_archive" = "$signed_expected_archive" ] || return 1
  [ -z "$signed_expected_commit" ] ||
    [ "$signed_manifest_commit" = "$signed_expected_commit" ] || return 1
  [ -z "$signed_expected_archive_sha256" ] ||
    [ "$signed_manifest_archive_sha256" = "$signed_expected_archive_sha256" ] || return 1
  [ -z "$signed_expected_archive_size" ] ||
    [ "$signed_manifest_archive_size" = "$signed_expected_archive_size" ] || return 1

  signed_manifest_canonical=$(mktemp \
    "$(dirname -- "$signed_manifest_path")/.release-manifest-v2.canonical.XXXXXXXX") \
    || return 1
  if ! printf '%s\n' \
    format=celikpanel-release-manifest-v2 \
    "sequence=$signed_sequence" \
    "version=$signed_version" \
    "commit=$signed_manifest_commit" \
    "published_at=$signed_published_at" \
    "os=$signed_os" \
    "arch=$signed_arch" \
    "archive=$signed_archive" \
    "archive_sha256=$signed_manifest_archive_sha256" \
    "archive_size=$signed_manifest_archive_size" \
      > "$signed_manifest_canonical"; then
    rm -f -- "$signed_manifest_canonical"
    return 1
  fi
  if ! cmp -s -- "$signed_manifest_path" "$signed_manifest_canonical"; then
    rm -f -- "$signed_manifest_canonical"
    return 1
  fi
  rm -f -- "$signed_manifest_canonical"

  signed_commit=$signed_manifest_commit
  signed_release_sequence=$signed_sequence
  signed_archive_sha256=$signed_manifest_archive_sha256
  signed_archive_size=$signed_manifest_archive_size
}

# END SIGNED RELEASE MANIFEST POLICY

# BEGIN DOWNLOAD OPERATION POLICY
interrupted_update_directory_chain_is_safe() {
  recovery_transaction_root=$1
  recovery_canonical=$(readlink -e -- "$recovery_transaction_root") || return 1
  [ "$recovery_canonical" = "$recovery_transaction_root" ] || return 1
  [ -d "$recovery_transaction_root" ] && [ ! -L "$recovery_transaction_root" ] || return 1
  [ "$(stat -Lc '%u:%g:%a' -- "$recovery_transaction_root")" = 0:0:700 ] || return 1

  recovery_current=$recovery_transaction_root
  while :; do
    [ -d "$recovery_current" ] && [ ! -L "$recovery_current" ] || return 1
    set -- $(stat -Lc '%u %g %a' -- "$recovery_current")
    [ "$1" -eq 0 ] && [ "$2" -eq 0 ] || return 1
    recovery_permissions=$((0$3))
    [ $((recovery_permissions & 0022)) -eq 0 ] || return 1
    [ "$recovery_current" = / ] && break
    recovery_current=$(dirname -- "$recovery_current")
  done
}

detect_known_interrupted_update_candidate_at() {
  recovery_transaction_root=$1
  recovery_alpha4_target=8bbbac8b628fae4fca0e127e52c1c7835f56f8b8
  interrupted_update_directory_chain_is_safe "$recovery_transaction_root" || return 1

  recovery_lock=$recovery_transaction_root/transaction.lock
  recovery_active=$recovery_transaction_root/active
  [ -f "$recovery_lock" ] && [ ! -L "$recovery_lock" ] || return 1
  [ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$recovery_lock")" = 0:0:600:1:0 ] || return 1
  [ -f "$recovery_active" ] && [ ! -L "$recovery_active" ] || return 1
  set -- $(stat -Lc '%u %g %a %h %s' -- "$recovery_active")
  [ "$1:$2:$3:$4" = 0:0:600:1 ] || return 1
  [ "$5" -ge 1 ] && [ "$5" -le 512 ] || return 1

  for recovery_phase_marker in \
    quiesce.pending completion.pending scheduler-restore.pending; do
    [ ! -e "$recovery_transaction_root/$recovery_phase_marker" ] && \
      [ ! -L "$recovery_transaction_root/$recovery_phase_marker" ] || return 1
  done

  recovery_version=
  recovery_token_line=
  recovery_operation=
  recovery_snapshot_line=
  recovery_extra=
  {
    IFS= read -r recovery_version || return 1
    IFS= read -r recovery_token_line || return 1
    IFS= read -r recovery_operation || return 1
    IFS= read -r recovery_snapshot_line || return 1
    if IFS= read -r recovery_extra || [ -n "$recovery_extra" ]; then
      return 1
    fi
  } < "$recovery_active"

  [ "$recovery_version" = version=1 ] || return 1
  recovery_token=${recovery_token_line#token=}
  [ "$recovery_token_line" = "token=$recovery_token" ] && \
    [ "${#recovery_token}" -eq 64 ] || return 1
  printf '%s\n' "$recovery_token" | LC_ALL=C grep -Eq '^[0-9a-f]{64}$' || return 1
  [ "$recovery_operation" = operation=update ] || return 1
  recovery_snapshot=${recovery_snapshot_line#snapshot=}
  [ "$recovery_snapshot_line" = "snapshot=$recovery_snapshot" ] || return 1
  printf '%s\n' "$recovery_snapshot" | LC_ALL=C grep -Eq \
    "^[0-9]{8}T[0-9]{6}Z-from-unknown-to-${recovery_alpha4_target}-[0-9a-f]{32}$" || return 1
}

detect_known_interrupted_update_candidate() {
  detect_known_interrupted_update_candidate_at \
    /var/lib/celikpanel-release-transaction
}

select_download_operation() {
  selection_requested=$1
  selection_marker_state=$2
  selection_full_install=$3
  selection_any_install=$4
  selection_panel_active=$5
  selection_agent_active=$6
  selection_recovery_candidate=$7

  if [ "$selection_requested" != auto ]; then
    printf '%s\n' "$selection_requested"
  elif [ "$selection_full_install" -eq 1 ] && \
    { [ "$selection_marker_state" = valid ] || \
      { [ "$selection_panel_active" -eq 1 ] && [ "$selection_agent_active" -eq 1 ]; }; }; then
    printf '%s\n' update
  elif [ "$selection_full_install" -eq 1 ] && \
    [ "$selection_marker_state" = absent ] && \
    [ "$selection_panel_active" -eq 0 ] && \
    [ "$selection_agent_active" -eq 0 ] && \
    [ "$selection_recovery_candidate" -eq 1 ]; then
    printf '%s\n' recovery-update
  elif [ "$selection_any_install" -eq 0 ]; then
    printf '%s\n' install
  else
    printf '%s\n' ambiguous
  fi
}
# END DOWNLOAD OPERATION POLICY

prepare_release_storage() {
  validate_root_directory_chain /var
  for directory in /var/backups /var/backups/celikpanel "$releases_root"; do
    if [ ! -e "$directory" ] && [ ! -L "$directory" ]; then
      desired_mode=0700
      [ "$directory" != /var/backups ] || desired_mode=0755
      install -d -m "$desired_mode" -o root -g root -- "$directory"
      sync -f -- "$(dirname -- "$directory")"
    fi
    validate_root_directory_chain "$directory"
  done
  set -- $(stat -Lc '%u %g %a' -- "$releases_root")
  [ "$1:$2:$3" = 0:0:700 ] || fail \
    "Release storage must be root:root mode 0700." \
    "Sürüm deposu root:root 0700 kipinde olmalı."
}

marker_state=absent
if [ -e /etc/celikpanel/install.complete ] || [ -L /etc/celikpanel/install.complete ]; then
  marker_state=invalid
  if [ -f /etc/celikpanel/install.complete ] && [ ! -L /etc/celikpanel/install.complete ]; then
    set -- $(stat -Lc '%u %g %a %h' -- /etc/celikpanel/install.complete)
    [ "$1:$2:$3:$4" = 0:0:600:1 ] && marker_state=valid
  fi
fi

full_install=1
for installed_path in \
  /opt/celikpanel/bin/panel \
  /opt/celikpanel/bin/agent \
  /etc/systemd/system/celikpanel-panel.service \
  /etc/systemd/system/celikpanel-agent.service \
  /etc/celikpanel/panel.env \
  /var/lib/celikpanel/celikpanel.db; do
  if [ ! -f "$installed_path" ] || [ -L "$installed_path" ]; then
    full_install=0
  fi
done

any_install=0
for installed_path in \
  /etc/celikpanel/install.complete \
  /opt/celikpanel/bin/panel \
  /opt/celikpanel/bin/agent \
  /etc/systemd/system/celikpanel-panel.service \
  /etc/systemd/system/celikpanel-agent.service \
  /etc/celikpanel/panel.env \
  /var/lib/celikpanel/celikpanel.db; do
  if [ -e "$installed_path" ] || [ -L "$installed_path" ]; then
    any_install=1
  fi
done

panel_active=0
agent_active=0
if command -v systemctl >/dev/null 2>&1; then
  systemctl is-active --quiet celikpanel-panel.service 2>/dev/null && panel_active=1 || true
  systemctl is-active --quiet celikpanel-agent.service 2>/dev/null && agent_active=1 || true
fi

recovery_candidate=0
detect_known_interrupted_update_candidate && recovery_candidate=1 || true
operation=$(select_download_operation \
  "$requested_action" "$marker_state" "$full_install" "$any_install" \
  "$panel_active" "$agent_active" "$recovery_candidate")
[ "$operation" != ambiguous ] || fail \
  "A partial or ambiguous CelikPanel installation was found. Retry with --install after a failed first setup, or use --update only after verifying the existing installation." \
  "Yarım veya belirsiz bir CelikPanel kurulumu bulundu. İlk kurulum başarısız olduysa --install ile yeniden deneyin; --update seçeneğini yalnız mevcut kurulumu doğruladıktan sonra kullanın."

if [ "$operation" = install ]; then
  [ "$marker_state" = absent ] && [ "$panel_active" -eq 0 ] || fail \
    "A completed or running CelikPanel installation already exists; use --update." \
    "Tamamlanmış veya çalışan bir CelikPanel kurulumu zaten var; --update kullanın."
else
  [ "$full_install" -eq 1 ] || fail \
    "The installed CelikPanel layout is incomplete; refusing an update." \
    "Kurulu CelikPanel yerleşimi eksik; güncelleme reddedildi."
  [ "$marker_state" != invalid ] || fail \
    "The installation-complete marker is unsafe; refusing an update." \
    "Kurulum-tamamlandı işareti güvenli değil; güncelleme reddedildi."
  if [ "$operation" = recovery-update ]; then
    detect_known_interrupted_update_candidate || fail \
      "The interrupted-update evidence changed; refusing automatic recovery." \
      "Kesilen güncelleme kanıtı değişti; otomatik kurtarma reddedildi."
  fi
fi

prepare_release_storage
workdir=$(mktemp -d "$releases_root/.download.XXXXXXXX")
chmod 0700 "$workdir"
chown root:root "$workdir"

if [ "$operation" = install ]; then
  bootstrap_signed_install=1
  signed_release_mode=1
  expected_sequence=$bootstrap_release_sequence
  minimum_sequence=$bootstrap_release_sequence
  case "$requested_version" in
    latest|"$bootstrap_release_version") ;;
    *) fail \
      "This installer is pinned to signed release $bootstrap_release_version." \
      "Bu kurucu imzalı $bootstrap_release_version sürümüne sabitlenmiştir." ;;
  esac
elif [ "$require_signed_manifest" -eq 1 ]; then
  signed_release_mode=1
else
  fail \
    "Existing installations must be updated from the panel's signed update screen." \
    "Mevcut kurulumlar paneldeki imzalı güncelleme ekranından güncellenmelidir."
fi

if [ "$bootstrap_signed_install" -eq 1 ]; then
  version=$bootstrap_release_version
elif [ "$requested_version" = latest ]; then
  curl_fetch "$base_url/releases/latest.txt" "$workdir/latest.txt"
  version=$(tr -d '\r\n\t ' < "$workdir/latest.txt")
else
  version=$requested_version
fi

valid_release_version "$version" || fail \
  "Unsafe or invalid release version: $version" \
  "Güvensiz veya geçersiz sürüm: $version"

archive=celikpanel-$version.tar.gz
release_url=$base_url/releases/$version
if [ "$signed_release_mode" -eq 1 ]; then
  if [ "$bootstrap_signed_install" -eq 1 ]; then
    signed_public_key_path=$workdir/release-signing-ed25519.pem
    signed_fetch "$base_url/release-signing-ed25519.pem" "$signed_public_key_path" 16384
    chown root:root -- "$signed_public_key_path"
    chmod 0600 -- "$signed_public_key_path"
    validate_bootstrap_release_public_key "$signed_public_key_path" || fail \
      "The release-signing public key does not match the installer trust anchor." \
      "Sürüm imzalama açık anahtarı kurucu güven köküyle eşleşmiyor."
  else
    acquire_signed_update_lock || fail \
      "Another signed update is active or the root-owned update lock is unsafe." \
      "Başka bir imzalı güncelleme etkin veya root sahipli güncelleme kilidi güvensiz."
    signed_public_key_path=$release_public_key
    validate_release_public_key "$signed_public_key_path" || fail \
      "The installed release-signing public key is unsafe or invalid." \
      "Kurulu sürüm imzalama açık anahtarı güvensiz veya geçersiz."
  fi
  runtime_release_identity || fail \
    "This operating system or architecture has no signed CelikPanel release channel." \
    "Bu işletim sistemi veya mimari için imzalı CelikPanel sürüm kanalı yok."
  archive=celikpanel-$version-$runtime_release_os-$runtime_release_arch.tar.gz
  release_url=$base_url/releases/$version/$runtime_release_os/$runtime_release_arch
  signed_fetch "$release_url/release-manifest-v2" "$workdir/release-manifest-v2" 4096
  signed_fetch "$release_url/release-manifest-v2.sig" "$workdir/release-manifest-v2.sig" 64
  verify_signed_release_manifest \
    "$workdir/release-manifest-v2" "$workdir/release-manifest-v2.sig" \
    "$signed_public_key_path" "$version" "$expected_sequence" "$runtime_release_os" \
    "$runtime_release_arch" "$archive" "$expected_commit" \
    "$expected_archive_sha256" "$expected_archive_size" || fail \
      "The signed release manifest is invalid or targets another system." \
      "İmzalı sürüm manifesti geçersiz veya başka bir sistemi hedefliyor."
  if [ "$bootstrap_signed_install" -eq 1 ]; then
    preflight_first_install_trust_state \
      "$signed_public_key_path" "$signed_release_sequence" "$version" || fail \
        "Existing signed-release trust conflicts with this fresh installation." \
        "Mevcut imzali surum guveni bu temiz kurulumla celisiyor."
    provision_first_install_signed_update_lock || fail \
      "The persistent first-install update lock could not be provisioned safely." \
      "Kalici ilk-kurulum guncelleme kilidi guvenle hazirlanamadi."
    acquire_signed_update_lock || fail \
      "Another signed update or first installation is active." \
      "Baska bir imzali guncelleme veya ilk kurulum etkin."
    preflight_first_install_trust_state \
      "$signed_public_key_path" "$signed_release_sequence" "$version" || fail \
        "Signed-release trust changed while acquiring the first-install lock." \
        "Ilk-kurulum kilidi alinirken imzali surum guveni degisti."
  else
    enforce_release_sequence_floor "$signed_release_sequence" "$version" || fail \
    "The signed release sequence is stale, unexpected, or lacks a trusted rollback floor." \
    "İmzalı sürüm sırası eski, beklenmeyen veya güvenilir geri-alma tabanından yoksun."
  fi
  signed_fetch "$release_url/$archive" "$workdir/$archive" "$signed_archive_size"
  signed_fetch "$release_url/$archive.sha256" "$workdir/$archive.sha256" 256
else
  curl_fetch "$release_url/$archive" "$workdir/$archive"
  curl_fetch "$release_url/$archive.sha256" "$workdir/$archive.sha256"
fi

expected_line=$(tr -d '\r' < "$workdir/$archive.sha256")
set -- $expected_line
[ "$#" -eq 2 ] || fail \
  "Checksum file has an unexpected format." \
  "Sağlama toplamı dosyası beklenmeyen biçimde."
checksum_value=$1
checksum_name=$2
[ "$checksum_name" = "$archive" ] && [ "${#checksum_value}" -eq 64 ] || fail \
  "Checksum file has an unexpected format." \
  "Sağlama toplamı dosyası beklenmeyen biçimde."
case "$checksum_value" in
  *[!0-9a-fA-F]*) fail \
    "Checksum file has an unexpected format." \
    "Sağlama toplamı dosyası beklenmeyen biçimde." ;;
esac
(cd "$workdir" && sha256sum -c "$archive.sha256")
if [ "$signed_release_mode" -eq 1 ]; then
  [ "$(stat -Lc '%s' -- "$workdir/$archive")" = "$signed_archive_size" ] || fail \
    "The archive size does not match the signed release manifest." \
    "Arşiv boyutu imzalı sürüm manifestiyle eşleşmiyor."
  [ "$(sha256sum "$workdir/$archive" | awk '{print $1}')" = "$signed_archive_sha256" ] || fail \
    "The archive digest does not match the signed release manifest." \
    "Arşiv özeti imzalı sürüm manifestiyle eşleşmiyor."
fi


root=celikpanel-$version
tar -tzf "$workdir/$archive" | awk -v root="$root" '
  BEGIN { ok = 1; count = 0 }
  {
    count++
    if ($0 ~ /^\// || $0 ~ /\\/ || $0 == ".." || $0 ~ /^\.\.\// || $0 ~ /\/\.\.($|\/)/) ok = 0
    if ($0 != root "/" && index($0, root "/") != 1) ok = 0
  }
  END { if (!ok || count == 0) exit 1 }
' || fail \
  "Archive contains unsafe or unexpected paths." \
  "Arşiv güvensiz veya beklenmeyen yollar içeriyor."

tar -tvzf "$workdir/$archive" | awk '
  { type = substr($0, 1, 1); if (type != "-" && type != "d") exit 1 }
' || fail \
  "Archive contains links or special filesystem objects." \
  "Arşiv bağlantı veya özel dosya sistemi nesneleri içeriyor."

mkdir "$workdir/extract"
tar -xzf "$workdir/$archive" -C "$workdir/extract" \
  --no-same-owner --no-same-permissions
extracted_root=$workdir/extract/$root
[ -d "$extracted_root" ] && [ ! -L "$extracted_root" ] || fail \
  "The verified archive does not contain the expected release root." \
  "Doğrulanan arşiv beklenen sürüm kökünü içermiyor."
if find "$extracted_root" -xdev -type l -print -quit | grep -q .; then
  fail "The extracted release contains a symbolic link." "Çıkarılan sürüm sembolik bağlantı içeriyor."
fi
if find "$extracted_root" -xdev ! -type d ! -type f -print -quit | grep -q .; then
  fail "The extracted release contains a special filesystem object." "Çıkarılan sürüm özel dosya sistemi nesnesi içeriyor."
fi
if find "$extracted_root" -xdev -type f -links +1 -print -quit | grep -q .; then
  fail "The extracted release contains a hard-linked file." "Çıkarılan sürüm hard-link dosyası içeriyor."
fi

manifest=$extracted_root/SHA256SUMS
[ -f "$manifest" ] && [ ! -L "$manifest" ] || fail \
  "The extracted release does not contain a regular SHA256SUMS manifest." \
  "Çıkarılan sürüm normal bir SHA256SUMS manifesti içermiyor."
(
  cd "$extracted_root"
  LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
    | LC_ALL=C sort -z \
    | xargs -0 sha256sum \
    | cmp -s - SHA256SUMS
  sha256sum -c SHA256SUMS >/dev/null
) || fail \
  "The extracted release does not match its exact internal checksum manifest." \
  "Çıkarılan sürüm birebir iç sağlama toplamı manifestiyle eşleşmiyor."

for metadata_name in release.version release.commit release.tree; do
  metadata_path=$extracted_root/$metadata_name
  [ -f "$metadata_path" ] && [ ! -L "$metadata_path" ] || fail \
    "The extracted release is missing verified provenance metadata." \
    "Çıkarılan sürümde doğrulanmış köken bilgisi eksik."
done
[ "$(tr -d '\r\n\t ' < "$extracted_root/release.version")" = 1 ] || fail \
  "The extracted release format is unsupported." \
  "Çıkarılan sürüm biçimi desteklenmiyor."
for metadata_name in release.commit release.tree; do
  metadata_value=$(tr -d '\r\n\t ' < "$extracted_root/$metadata_name")
  printf '%s\n' "$metadata_value" | grep -Eq '^[0-9a-f]{40,64}$' || fail \
    "The extracted release contains invalid provenance metadata." \
    "Çıkarılan sürüm geçersiz köken bilgisi içeriyor."
done
if [ "$signed_release_mode" -eq 1 ]; then
  extracted_release_commit=$(tr -d '\r\n\t ' < "$extracted_root/release.commit")
  [ "$extracted_release_commit" = "$signed_commit" ] || fail \
    "The extracted release commit does not match the signed release manifest." \
    "Çıkarılan sürüm commit bilgisi imzalı sürüm manifestiyle eşleşmiyor."
fi
if [ "$require_signed_manifest" -eq 1 ]; then
  # Consume the authenticated sequence only after the entire archive and its
  # provenance are verified, but before the updater can mutate host services.
  # A failed update can safely retry the same sequence/version; lower or same
  # sequence with different version remains permanently rejected.
  persist_release_sequence_floor "$signed_release_sequence" "$version" || fail \
    "The durable signed-release rollback floor could not be published safely." \
    "Kalıcı imzalı sürüm geri-alma tabanı güvenle yayımlanamadı."
fi

if [ "$operation" = install ]; then
  installer=$extracted_root/install.sh
  [ -f "$installer" ] && [ ! -L "$installer" ] || fail \
    "The verified archive does not contain a regular install.sh." \
    "Doğrulanan arşiv normal bir install.sh dosyası içermiyor."
  validate_bootstrap_release_public_key "$signed_public_key_path" || fail \
    "The pinned first-install release key changed before installer handoff." \
    "Sabitlenmis ilk-kurulum surum anahtari installer aktarimindan once degisti."
  message \
    "Installing CelikPanel $version from verified archive $archive" \
    "CelikPanel $version doğrulanmış $archive arşivinden kuruluyor"
  cd "$extracted_root"
  CELIKPANEL_FIRST_INSTALL_TRUST=1 \
    CELIKPANEL_FIRST_INSTALL_PUBLIC_KEY_FILE="$signed_public_key_path" \
    CELIKPANEL_FIRST_INSTALL_SEQUENCE="$signed_release_sequence" \
    CELIKPANEL_FIRST_INSTALL_VERSION="$version" \
    CELIKPANEL_FIRST_INSTALL_COMMIT="$signed_commit" \
    CELIKPANEL_FIRST_INSTALL_LOCK_FD=9 \
    bash "$installer"
else
  if [ "$operation" = recovery-update ]; then
    detect_known_interrupted_update_candidate || fail \
      "The interrupted-update evidence changed while downloading the release; refusing automatic recovery." \
      "Sürüm indirilirken kesilen güncelleme kanıtı değişti; otomatik kurtarma reddedildi."
  fi
  updater=$extracted_root/bootstrap-prebuilt-update.sh
  [ -f "$updater" ] && [ ! -L "$updater" ] || fail \
    "The verified archive does not contain the prebuilt update entry point." \
    "Doğrulanan arşiv hazır güncelleme giriş noktasını içermiyor."
  message \
    "Updating CelikPanel to $version from verified prebuilt archive $archive" \
    "CelikPanel doğrulanmış hazır $archive arşivinden $version sürümüne güncelleniyor"
  bash "$updater" --normal
  install -m 0600 -o root -g root /dev/null /etc/celikpanel/install.complete
  sync -f /etc/celikpanel/install.complete /etc/celikpanel
fi
