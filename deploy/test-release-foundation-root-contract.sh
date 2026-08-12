#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "$0")/.." && pwd)
installer=$repo_root/install.sh
bootstrap=$repo_root/download-portal/get.sh

fail() {
    printf 'release foundation root contract failed: %s\n' "$1" >&2
    exit 1
}

[[ $(id -u) -eq 0 ]] || fail 'this contract must run as root'
for command in awk flock stat timeout; do
    command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done

test_root=$(mktemp -d /tmp/celikpanel-release-foundation.XXXXXXXX)
trap 'rm -rf -- "$test_root"' EXIT
chmod 0700 "$test_root"
policy=$test_root/lock-policy.sh

extract_function() {
    local source_file=$1 function_name=$2
    awk -v wanted="$function_name" '
        $0 ~ "^" wanted "\\(\\) \\{" {
            active = 1
        }
        active {
            print
            opens = gsub(/\{/, "{")
            closes = gsub(/\}/, "}")
            depth += opens - closes
            if (depth == 0) {
                exit
            }
        }
    ' "$source_file"
}

{
    extract_function "$installer" provision_signed_update_lock
    extract_function "$bootstrap" acquire_signed_update_lock
} > "$policy"
bash -n "$policy" || fail 'extracted lock policy has invalid syntax'
# shellcheck disable=SC1090
source "$policy"

die() {
    printf '%s\n' "$1" >&2
    exit 1
}

# Production validates the fixed /var/lib chain. This extracted test redirects
# only the function globals into mktemp; no production alternate-root input
# exists.
validate_release_state_parent_chain() {
    :
}
validate_root_directory_chain() {
    :
}

RELEASE_STATE_DIR=$test_root/release-state
SIGNED_UPDATE_LOCK=$RELEASE_STATE_DIR/update.lock
signed_update_lock=$SIGNED_UPDATE_LOCK
export RELEASE_STATE_DIR SIGNED_UPDATE_LOCK signed_update_lock
export -f provision_signed_update_lock acquire_signed_update_lock \
    validate_release_state_parent_chain validate_root_directory_chain die

# Many simultaneous first installers must converge on one O_EXCL-created inode.
for attempt in $(seq 1 24); do
    (
        provision_signed_update_lock
        stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK" > "$test_root/inode.$attempt"
    ) &
done
wait
[[ $(sort -u "$test_root"/inode.* | wc -l) -eq 1 ]] \
    || fail 'concurrent creators observed more than one lock inode'
[[ "$(stat -Lc '%u:%g:%a' -- "$RELEASE_STATE_DIR")" == 0:0:700 ]] \
    || fail 'release-state directory metadata is not exact'
[[ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$SIGNED_UPDATE_LOCK")" == 0:0:600:1:0 ]] \
    || fail 'signed-update lock metadata is not exact'

stable_inode=$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")
provision_signed_update_lock
[[ "$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")" == "$stable_inode" ]] \
    || fail 'idempotent provisioning replaced the lock inode'

# Recover only the safe root:effective-gid crash window, then normalize to root.
chown 0:65534 -- "$RELEASE_STATE_DIR" "$SIGNED_UPDATE_LOCK"
id() {
    [[ $# -eq 1 && $1 == -g ]] || return 1
    printf '65534\n'
}
provision_signed_update_lock
unset -f id
[[ "$(stat -Lc '%u:%g:%a' -- "$RELEASE_STATE_DIR")" == 0:0:700 ]] \
    || fail 'safe directory crash artifact was not normalized'
[[ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$SIGNED_UPDATE_LOCK")" == 0:0:600:1:0 ]] \
    || fail 'safe lock crash artifact was not normalized'

# This is the signed get.sh -> inherited fd9 -> apply-only installer boundary:
# installer validation must finish without taking a second blocking flock.
acquire_signed_update_lock || fail 'signed updater could not acquire the exact lock'
timeout 5 bash -c 'provision_signed_update_lock' \
    || fail 'apply-only installer blocked on the inherited signed-update lock'
[[ "$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")" == "$stable_inode" ]] \
    || fail 'apply-only validation replaced the held lock inode'
exec 9>&-

chmod 0660 "$SIGNED_UPDATE_LOCK"
if (provision_signed_update_lock >/dev/null 2>&1); then
    fail 'group-writable lock metadata was accepted'
fi
chmod 0600 "$SIGNED_UPDATE_LOCK"
printf 'x' > "$SIGNED_UPDATE_LOCK"
if (provision_signed_update_lock >/dev/null 2>&1); then
    fail 'non-empty lock file was accepted'
fi
: > "$SIGNED_UPDATE_LOCK"
chmod 0750 "$RELEASE_STATE_DIR"
if (provision_signed_update_lock >/dev/null 2>&1); then
    fail 'non-exact release-state directory mode was accepted'
fi

printf 'release foundation root contract passed\n'
