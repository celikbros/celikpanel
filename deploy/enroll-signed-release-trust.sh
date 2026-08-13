#!/usr/bin/env bash
# One-time operator-authorized enrollment for CelikPanel's signed update trust.
# This script is run from an authenticated reviewed checkout. It is not invoked
# by install.sh, update.sh, the panel, or the agent.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

PANEL_BIN=/opt/celikpanel/bin/panel
AGENT_BIN=/opt/celikpanel/bin/agent
RELEASE_PUBLIC_KEY=/etc/celikpanel/release-signing-ed25519.pem
RELEASE_STATE_DIR=/var/lib/celikpanel-release-state
RELEASE_SEQUENCE_FLOOR="$RELEASE_STATE_DIR/sequence.floor"
SIGNED_UPDATE_LOCK="$RELEASE_STATE_DIR/update.lock"
BINARY_TRUST_ANCHOR=/
KEY_TRUST_ANCHOR=/
STATE_TRUST_ANCHOR=/

workdir=
staged_key=
staged_floor=
enrollment_lock_fd=
PUBLIC_KEY_SOURCE_FD=

usage() {
  cat >&2 <<'USAGE'
Usage: sudo bash deploy/enroll-signed-release-trust.sh \
  --sequence N --version VERSION --commit COMMIT \
  --public-key-file /canonical/root-owned/ed25519-public-key.pem
USAGE
  exit 2
}

die() {
  printf 'signed release trust enrollment failed: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [[ -n "$staged_key" && ( -e "$staged_key" || -L "$staged_key" ) ]]; then
    rm -f -- "$staged_key"
  fi
  if [[ -n "$staged_floor" && ( -e "$staged_floor" || -L "$staged_floor" ) ]]; then
    rm -f -- "$staged_floor"
  fi
  if [[ -n "$enrollment_lock_fd" ]]; then
    eval "exec ${enrollment_lock_fd}>&-"
  fi
  if [[ -n "$PUBLIC_KEY_SOURCE_FD" ]]; then
    eval "exec ${PUBLIC_KEY_SOURCE_FD}<&-"
  fi
  if [[ -n "$workdir" && -d "$workdir" && ! -L "$workdir" ]]; then
    case "$workdir" in
      /tmp/celikpanel-release-enrollment.*) rm -rf -- "$workdir" ;;
    esac
  fi
}

on_signal() {
  local status=$1
  trap - HUP INT TERM
  exit "$status"
}

trap cleanup EXIT
trap 'on_signal 129' HUP
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

valid_release_version() {
  local value=$1 prerelease identifier
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

valid_release_sequence() {
  local value=$1
  [[ "$value" =~ ^[1-9][0-9]*$ && ${#value} -le 19 ]] || return 1
  (( 10#$value <= 9223372036854775807 ))
}

path_is_beneath_anchor() {
  local path=$1 anchor=$2
  case "$anchor" in
    /) [[ "$path" == /* ]] ;;
    *) [[ "$path" == "$anchor" || "$path" == "$anchor"/* ]] ;;
  esac
}

validate_root_directory_chain() {
  local path=$1 anchor=$2 current parent canonical owner group mode permissions
  path_is_beneath_anchor "$path" "$anchor" \
    || die "trusted path escaped its authority root: $path"
  current=$path
  while true; do
    [[ -d "$current" && ! -L "$current" ]] \
      || die "trusted directory is missing or symbolic: $current"
    canonical=$(readlink -e -- "$current") \
      || die "cannot resolve trusted directory: $current"
    [[ "$canonical" == "$current" ]] \
      || die "trusted directory is not canonical: $current"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
      || die "cannot inspect trusted directory: $current"
    permissions=$((8#$mode))
    [[ "$owner:$group" == 0:0 && $((permissions & 0022)) -eq 0 ]] \
      || die "trusted directory must be root:root and group/other non-writable: $current"
    [[ "$current" == "$anchor" ]] && break
    parent=$(dirname -- "$current")
    [[ "$parent" != "$current" ]] \
      || die "trusted directory anchor was not reached: $path"
    current=$parent
  done
}

validate_key_target_directory_chain() {
  local path=$1 anchor=$2 current parent canonical owner group mode permissions direct=1
  path_is_beneath_anchor "$path" "$anchor" \
    || die "release key directory escaped its authority root: $path"
  current=$path
  while true; do
    [[ -d "$current" && ! -L "$current" ]] \
      || die "release key directory is missing or symbolic: $current"
    canonical=$(readlink -e -- "$current") \
      || die "cannot resolve release key directory: $current"
    [[ "$canonical" == "$current" ]] \
      || die "release key directory is not canonical: $current"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
      || die "cannot inspect release key directory: $current"
    permissions=$((8#$mode))
    [[ "$owner" == 0 && $((permissions & 0022)) -eq 0 ]] \
      || die "release key directory must be root-owned and group/other non-writable: $current"
    if (( ! direct )); then
      [[ "$group" == 0 ]] \
        || die "release key ancestor must be root:root: $current"
    fi
    [[ "$current" == "$anchor" ]] && break
    direct=0
    parent=$(dirname -- "$current")
    [[ "$parent" != "$current" ]] \
      || die "release key directory anchor was not reached: $path"
    current=$parent
  done
}

validate_installed_binary() {
  local role=$1 path=$2 output_file=$3 canonical owner group mode links size
  local binary_fd binary_fd_path path_identity fd_identity version_line commit_line extra
  validate_root_directory_chain "$(dirname -- "$path")" "$BINARY_TRUST_ANCHOR"
  [[ -f "$path" && ! -L "$path" ]] \
    || die "installed $role binary is missing or unsafe"
  canonical=$(readlink -e -- "$path") \
    || die "cannot resolve installed $role binary"
  [[ "$canonical" == "$path" ]] \
    || die "installed $role binary path is not canonical"
  exec {binary_fd}<"$path" || die "cannot open installed $role binary"
  binary_fd_path=/proc/self/fd/$binary_fd
  path_identity=$(stat -Lc '%d:%i' -- "$path") \
    || die "cannot identify installed $role binary path"
  fd_identity=$(stat -Lc '%d:%i' -- "$binary_fd_path") \
    || die "cannot identify opened $role binary"
  [[ "$path_identity" == "$fd_identity" ]] \
    || die "installed $role binary changed while opening"
  read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$binary_fd_path") \
    || die "cannot inspect installed $role binary"
  [[ "$owner:$group:$mode:$links" == 0:0:755:1 && "$size" -ge 1 && "$size" -le 268435456 ]] \
    || die "installed $role binary metadata is unsafe"
  timeout 15 "$binary_fd_path" --inspect-build-identity > "$output_file" \
    || die "installed $role binary did not return its build identity"
  [[ "$(stat -Lc '%d:%i' -- "$path")" == "$fd_identity" ]] \
    || die "installed $role binary changed during identity proof"
  [[ "$(stat -Lc '%s' -- "$output_file")" -ge 1 && "$(stat -Lc '%s' -- "$output_file")" -le 256 ]] \
    || die "installed $role build identity has an invalid size"
  version_line= commit_line= extra=
  {
    IFS= read -r version_line || die "installed $role build identity is incomplete"
    IFS= read -r commit_line || die "installed $role build identity is incomplete"
    if IFS= read -r extra || [[ -n "$extra" ]]; then
      die "installed $role build identity has extra data"
    fi
  } < "$output_file"
  INSPECTED_VERSION=${version_line#version=}
  INSPECTED_COMMIT=${commit_line#commit=}
  [[ "$version_line" == "version=$INSPECTED_VERSION" &&
     "$commit_line" == "commit=$INSPECTED_COMMIT" ]] \
    || die "installed $role build identity fields are invalid"
  valid_release_version "$INSPECTED_VERSION" \
    || die "installed $role version is not canonical"
  [[ "$INSPECTED_COMMIT" =~ ^[0-9a-f]{40}$ ]] \
    || die "installed $role commit is not canonical"
  printf 'version=%s\ncommit=%s\n' "$INSPECTED_VERSION" "$INSPECTED_COMMIT" \
    | cmp -s - "$output_file" \
    || die "installed $role build identity bytes are not canonical"
  eval "exec ${binary_fd}<&-"
}

validate_public_key_source() {
  local source=$1 canonical owner group mode links size permissions
  validate_root_directory_chain "$(dirname -- "$source")" "$KEY_TRUST_ANCHOR"
  [[ -f "$source" && ! -L "$source" ]] \
    || die "operator public key is missing or unsafe"
  canonical=$(readlink -e -- "$source") \
    || die "cannot resolve operator public key"
  [[ "$canonical" == "$source" ]] \
    || die "operator public key path must be canonical"
  if [[ -n "$PUBLIC_KEY_SOURCE_FD" ]]; then
    eval "exec ${PUBLIC_KEY_SOURCE_FD}<&-"
    PUBLIC_KEY_SOURCE_FD=
  fi
  exec {PUBLIC_KEY_SOURCE_FD}<"$source" || die "cannot open operator public key"
  PUBLIC_KEY_SOURCE_FD_PATH=/proc/self/fd/$PUBLIC_KEY_SOURCE_FD
  PUBLIC_KEY_SOURCE_IDENTITY=$(stat -Lc '%d:%i' -- "$PUBLIC_KEY_SOURCE_FD_PATH") \
    || die "cannot identify opened operator public key"
  [[ "$(stat -Lc '%d:%i' -- "$source")" == "$PUBLIC_KEY_SOURCE_IDENTITY" ]] \
    || die "operator public key changed while opening"
  read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$PUBLIC_KEY_SOURCE_FD_PATH") \
    || die "cannot inspect operator public key"
  permissions=$((8#$mode))
  [[ "$owner:$group" == 0:0 && "$links" == 1 && "$size" -ge 1 && "$size" -le 16384 &&
     $((permissions & 0022)) -eq 0 ]] \
    || die "operator public key metadata is unsafe"
  openssl pkey -pubin -passin pass: -in "$PUBLIC_KEY_SOURCE_FD_PATH" -pubout 2>/dev/null \
    | cmp -s - "$PUBLIC_KEY_SOURCE_FD_PATH" \
    || die "operator public key must be canonical PEM"
  openssl pkey -pubin -passin pass: -in "$PUBLIC_KEY_SOURCE_FD_PATH" -text -noout 2>/dev/null \
    | LC_ALL=C grep -Eq '^ED25519 Public-Key:' \
    || die "operator public key must be Ed25519"
}

ensure_release_state_and_lock() {
  local parent owner group mode links size path_identity fd_identity created=0
  local effective_gid state_created=0
  effective_gid=$(id -g) || die "cannot determine the enrollment effective group"
  parent=$(dirname -- "$RELEASE_STATE_DIR")
  validate_root_directory_chain "$parent" "$STATE_TRUST_ANCHOR"
  if [[ ! -e "$RELEASE_STATE_DIR" && ! -L "$RELEASE_STATE_DIR" ]]; then
    mkdir -m 0700 -- "$RELEASE_STATE_DIR" \
      || [[ -d "$RELEASE_STATE_DIR" && ! -L "$RELEASE_STATE_DIR" ]] \
      || die "cannot create release state directory"
    chown root:root -- "$RELEASE_STATE_DIR" \
      || die "cannot set release state directory ownership"
    chmod 0700 -- "$RELEASE_STATE_DIR" \
      || die "cannot set release state directory mode"
    sync -f -- "$RELEASE_STATE_DIR" "$parent" \
      || die "cannot make release state directory durable"
  fi
  [[ -d "$RELEASE_STATE_DIR" && ! -L "$RELEASE_STATE_DIR" &&
     "$(readlink -e -- "$RELEASE_STATE_DIR")" == "$RELEASE_STATE_DIR" ]] \
    || die "release state directory metadata is unsafe"
  read -r owner group mode < <(stat -Lc '%u %g %a' -- "$RELEASE_STATE_DIR") \
    || die "cannot inspect release state directory"
  [[ "$owner" == 0 && "$mode" == 700 &&
     ( "$group" == 0 || "$group" == "$effective_gid" ) ]] \
    || die "release state directory metadata is unsafe"
  if [[ "$group" != 0 ]]; then
    chown root:root -- "$RELEASE_STATE_DIR" \
      || die "cannot recover release state directory ownership"
    state_created=1
  fi
  [[ "$(stat -Lc '%u:%g:%a' -- "$RELEASE_STATE_DIR")" == 0:0:700 ]] \
    || die "release state directory metadata could not be normalized"
  if (( state_created )); then
    sync -f -- "$RELEASE_STATE_DIR" "$parent" \
      || die "cannot make recovered release state directory durable"
  fi
  if [[ ! -e "$SIGNED_UPDATE_LOCK" && ! -L "$SIGNED_UPDATE_LOCK" ]]; then
    if (set -o noclobber; : > "$SIGNED_UPDATE_LOCK") 2>/dev/null; then
      created=1
      chown root:root -- "$SIGNED_UPDATE_LOCK" \
        || die "cannot set signed update lock ownership"
      chmod 0600 -- "$SIGNED_UPDATE_LOCK" \
        || die "cannot set signed update lock mode"
      sync -f -- "$SIGNED_UPDATE_LOCK" "$RELEASE_STATE_DIR" \
        || die "cannot make signed update lock durable"
    elif [[ ! -e "$SIGNED_UPDATE_LOCK" && ! -L "$SIGNED_UPDATE_LOCK" ]]; then
      die "cannot create signed update lock"
    fi
  fi
  [[ -f "$SIGNED_UPDATE_LOCK" && ! -L "$SIGNED_UPDATE_LOCK" &&
     "$(readlink -e -- "$SIGNED_UPDATE_LOCK")" == "$SIGNED_UPDATE_LOCK" ]] \
    || die "signed update lock is missing or unsafe"
  exec {enrollment_lock_fd}<>"$SIGNED_UPDATE_LOCK" \
    || die "cannot open signed update lock"
  path_identity=$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK") \
    || die "cannot identify signed update lock path"
  fd_identity=$(stat -Lc '%d:%i' -- "/proc/self/fd/$enrollment_lock_fd") \
    || die "cannot identify opened signed update lock"
  [[ "$path_identity" == "$fd_identity" ]] \
    || die "signed update lock changed while opening"
  read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "/proc/self/fd/$enrollment_lock_fd") \
    || die "cannot inspect signed update lock"
  [[ "$owner" == 0 && "$mode" == 600 && "$links" == 1 && "$size" == 0 &&
     ( "$group" == 0 || "$group" == "$effective_gid" ) ]] \
    || die "signed update lock metadata is unsafe"
  if [[ "$group" != 0 ]]; then
    [[ "$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")" == "$fd_identity" ]] \
      || die "signed update lock changed before ownership recovery"
    chown root:root -- "$SIGNED_UPDATE_LOCK" \
      || die "cannot recover signed update lock ownership"
    sync -f -- "$SIGNED_UPDATE_LOCK" "$RELEASE_STATE_DIR" \
      || die "cannot make recovered signed update lock durable"
  fi
  [[ "$(stat -Lc '%u:%g:%a:%h:%s' -- "/proc/self/fd/$enrollment_lock_fd")" == 0:0:600:1:0 ]] \
    || die "signed update lock metadata could not be normalized"
  flock -n "$enrollment_lock_fd" \
    || die "another signed update or trust enrollment is active"
  [[ "$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")" == "$fd_identity" ]] \
    || die "signed update lock changed after acquisition"
  : "$created"
}

cleanup_safe_stages() {
  local directory=$1 prefix=$2 max_size=$3 allowed_modes=$4 stage metadata removed=0
  local creator_group
  creator_group=$(id -g) || die "cannot determine enrollment staging group"
  shopt -s nullglob
  for stage in "$directory"/"$prefix"????????; do
    [[ -f "$stage" && ! -L "$stage" ]] \
      || die "unsafe enrollment staging artifact: $stage"
    metadata=$(stat -Lc '%u:%g:%a:%h:%s' -- "$stage") \
      || die "cannot inspect enrollment staging artifact"
    IFS=: read -r stage_owner stage_group stage_mode stage_links stage_size <<< "$metadata"
    [[ "$stage_owner" == 0 &&
       ( "$stage_group" == 0 ||
         ( "$stage_group" == "$creator_group" && "$stage_mode" == 600 ) ) &&
       "$stage_links" == 1 &&
       " $allowed_modes " == *" $stage_mode "* &&
       "$stage_size" -le "$max_size" ]] \
      || die "unsafe enrollment staging artifact metadata: $stage"
    rm -- "$stage" || die "cannot remove stale enrollment staging artifact"
    removed=1
  done
  shopt -u nullglob
  if (( removed )); then
    sync -f -- "$directory" || die "cannot make enrollment stage cleanup durable"
  fi
}

inspect_existing_floor() {
  local expected_sequence=$1 expected_version=$2 format_line sequence_line version_line extra
  FLOOR_PRESENT=0
  if [[ ! -e "$RELEASE_SEQUENCE_FLOOR" && ! -L "$RELEASE_SEQUENCE_FLOOR" ]]; then
    return 0
  fi
  [[ -f "$RELEASE_SEQUENCE_FLOOR" && ! -L "$RELEASE_SEQUENCE_FLOOR" &&
     "$(readlink -e -- "$RELEASE_SEQUENCE_FLOOR")" == "$RELEASE_SEQUENCE_FLOOR" &&
     "$(stat -Lc '%u:%g:%a:%h' -- "$RELEASE_SEQUENCE_FLOOR")" == 0:0:600:1 &&
     "$(stat -Lc '%s' -- "$RELEASE_SEQUENCE_FLOOR")" -ge 1 &&
     "$(stat -Lc '%s' -- "$RELEASE_SEQUENCE_FLOOR")" -le 512 ]] \
    || die "existing release sequence floor metadata is unsafe"
  format_line= sequence_line= version_line= extra=
  {
    IFS= read -r format_line || die "existing release sequence floor is incomplete"
    IFS= read -r sequence_line || die "existing release sequence floor is incomplete"
    IFS= read -r version_line || die "existing release sequence floor is incomplete"
    if IFS= read -r extra || [[ -n "$extra" ]]; then
      die "existing release sequence floor has extra data"
    fi
  } < "$RELEASE_SEQUENCE_FLOOR"
  [[ "$format_line" == format=celikpanel-release-sequence-floor-v1 &&
     "$sequence_line" == "sequence=$expected_sequence" &&
     "$version_line" == "version=$expected_version" ]] \
    || die "existing release sequence floor conflicts with the approved identity"
  printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
    "sequence=$expected_sequence" "version=$expected_version" \
    | cmp -s - "$RELEASE_SEQUENCE_FLOOR" \
    || die "existing release sequence floor bytes are not canonical"
  FLOOR_PRESENT=1
}

inspect_existing_public_key() {
  KEY_PRESENT=0
  if [[ ! -e "$RELEASE_PUBLIC_KEY" && ! -L "$RELEASE_PUBLIC_KEY" ]]; then
    return 0
  fi
  [[ -f "$RELEASE_PUBLIC_KEY" && ! -L "$RELEASE_PUBLIC_KEY" &&
     "$(readlink -e -- "$RELEASE_PUBLIC_KEY")" == "$RELEASE_PUBLIC_KEY" &&
     "$(stat -Lc '%u:%g:%a:%h' -- "$RELEASE_PUBLIC_KEY")" == 0:0:644:1 &&
     "$(stat -Lc '%s' -- "$RELEASE_PUBLIC_KEY")" -ge 1 &&
     "$(stat -Lc '%s' -- "$RELEASE_PUBLIC_KEY")" -le 16384 ]] \
    || die "existing release public key metadata is unsafe"
  cmp -s -- "$PUBLIC_KEY_SOURCE_FD_PATH" "$RELEASE_PUBLIC_KEY" \
    || die "existing release public key differs; replacement is refused"
  KEY_PRESENT=1
}

publish_public_key() {
  local directory
  (( KEY_PRESENT == 0 )) || return 0
  directory=$(dirname -- "$RELEASE_PUBLIC_KEY")
  staged_key=$(mktemp "$directory/.release-signing-ed25519.pem.enroll.XXXXXXXX") \
    || die "cannot stage release public key"
  cp --no-preserve=mode,ownership,timestamps -- "$PUBLIC_KEY_SOURCE_FD_PATH" "$staged_key" \
    || die "cannot copy release public key"
  chown root:root -- "$staged_key" && chmod 0644 -- "$staged_key" \
    || die "cannot set staged release public key metadata"
  cmp -s -- "$PUBLIC_KEY_SOURCE_FD_PATH" "$staged_key" \
    || die "staged release public key bytes differ"
  sync -f -- "$staged_key" || die "cannot make staged release public key durable"
  mv -T -n -- "$staged_key" "$RELEASE_PUBLIC_KEY" \
    || die "cannot publish release public key"
  if [[ -e "$staged_key" || -L "$staged_key" ]]; then
    die "release public key appeared concurrently; replacement is refused"
  fi
  staged_key=
  sync -f -- "$directory" || die "cannot make release public key entry durable"
}

publish_sequence_floor() {
  local sequence=$1 version=$2
  (( FLOOR_PRESENT == 0 )) || return 0
  staged_floor=$(mktemp "$RELEASE_STATE_DIR/.sequence.floor.enroll.XXXXXXXX") \
    || die "cannot stage release sequence floor"
  printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
    "sequence=$sequence" "version=$version" > "$staged_floor" \
    || die "cannot write staged release sequence floor"
  chown root:root -- "$staged_floor" && chmod 0600 -- "$staged_floor" \
    || die "cannot set staged release sequence floor metadata"
  sync -f -- "$staged_floor" || die "cannot make staged release sequence floor durable"
  mv -T -n -- "$staged_floor" "$RELEASE_SEQUENCE_FLOOR" \
    || die "cannot publish release sequence floor"
  if [[ -e "$staged_floor" || -L "$staged_floor" ]]; then
    die "release sequence floor appeared concurrently; replacement is refused"
  fi
  staged_floor=
  sync -f -- "$RELEASE_STATE_DIR" \
    || die "cannot make release sequence floor entry durable"
}

main() {
  local sequence= version= commit= public_key_file= panel_version panel_commit
  local agent_version agent_commit
  while (( $# )); do
    case "$1" in
      --sequence)
        [[ -z "$sequence" && $# -ge 2 ]] || usage
        sequence=$2; shift 2 ;;
      --version)
        [[ -z "$version" && $# -ge 2 ]] || usage
        version=$2; shift 2 ;;
      --commit)
        [[ -z "$commit" && $# -ge 2 ]] || usage
        commit=$2; shift 2 ;;
      --public-key-file)
        [[ -z "$public_key_file" && $# -ge 2 ]] || usage
        public_key_file=$2; shift 2 ;;
      --help|-h) usage ;;
      *) usage ;;
    esac
  done
  [[ -n "$sequence" && -n "$version" && -n "$commit" && -n "$public_key_file" ]] \
    || usage
  [[ $(id -u) -eq 0 ]] || die "enrollment must run as root"
  valid_release_sequence "$sequence" || die "release sequence is not canonical"
  valid_release_version "$version" || die "release version is not canonical"
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || die "release commit is not canonical"
  for command in chmod chown cmp dirname flock grep id mktemp mv openssl readlink rm stat sync timeout; do
    command -v "$command" >/dev/null 2>&1 || die "$command is required"
  done
  workdir=$(mktemp -d /tmp/celikpanel-release-enrollment.XXXXXXXX) \
    || die "cannot create enrollment work directory"
  chown root:root -- "$workdir" && chmod 0700 -- "$workdir" \
    || die "cannot secure enrollment work directory"

  validate_installed_binary panel "$PANEL_BIN" "$workdir/panel.identity"
  panel_version=$INSPECTED_VERSION; panel_commit=$INSPECTED_COMMIT
  validate_installed_binary agent "$AGENT_BIN" "$workdir/agent.identity"
  agent_version=$INSPECTED_VERSION; agent_commit=$INSPECTED_COMMIT
  [[ "$panel_version" == "$agent_version" && "$panel_commit" == "$agent_commit" ]] \
    || die "installed panel and agent build identities differ"
  [[ "$panel_version" == "$version" && "$panel_commit" == "$commit" ]] \
    || die "installed build identity differs from the operator-approved identity"
  validate_public_key_source "$public_key_file"
  validate_key_target_directory_chain "$(dirname -- "$RELEASE_PUBLIC_KEY")" "$KEY_TRUST_ANCHOR"

  ensure_release_state_and_lock
  # Re-prove every external identity only after owning the persistent update
  # lock. No update can cross the proof-to-publication boundary.
  validate_installed_binary panel "$PANEL_BIN" "$workdir/panel.locked.identity"
  [[ "$INSPECTED_VERSION:$INSPECTED_COMMIT" == "$version:$commit" ]] \
    || die "installed panel identity changed before enrollment"
  validate_installed_binary agent "$AGENT_BIN" "$workdir/agent.locked.identity"
  [[ "$INSPECTED_VERSION:$INSPECTED_COMMIT" == "$version:$commit" ]] \
    || die "installed agent identity changed before enrollment"
  [[ "$(stat -Lc '%d:%i' -- "$public_key_file")" == "$PUBLIC_KEY_SOURCE_IDENTITY" ]] \
    || die "operator public key changed before enrollment"
  validate_public_key_source "$public_key_file"

  cleanup_safe_stages "$(dirname -- "$RELEASE_PUBLIC_KEY")" \
    '.release-signing-ed25519.pem.enroll.' 16384 '600 644'
  cleanup_safe_stages "$RELEASE_STATE_DIR" '.sequence.floor.enroll.' 512 '600'
  inspect_existing_floor "$sequence" "$version"
  inspect_existing_public_key
  publish_public_key
  publish_sequence_floor "$sequence" "$version"
  inspect_existing_public_key
  inspect_existing_floor "$sequence" "$version"
  printf 'Signed release trust enrolled for %s at sequence %s (%s).\n' \
    "$version" "$sequence" "$commit"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main "$@"
fi
