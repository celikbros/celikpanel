#!/usr/bin/env bash
# Recover only the database-snapshot step of an already active normal update.
# The durable transaction remains active; this tool never advances its phase or
# starts either coordinator. The operator must rerun the exact target updater.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

RELEASES_ROOT=/var/backups/celikpanel/releases
SNAPSHOT_ROOT=/var/backups/celikpanel/update-snapshots
RECOVERY_SNAPSHOT_ROOT=/var/backups/celikpanel/recovery-snapshots
TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
RELEASE_TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
RELEASE_TRANSACTION_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
UNIT_DIR=/etc/systemd/system
SYSTEMCTL_BIN=/usr/bin/systemctl
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
AGENT_STATE_DIR=/var/lib/celikpanel-agent-private
PANEL_DATA_DIR=/var/lib/celikpanel
PANEL_DB=$PANEL_DATA_DIR/celikpanel.db

ACTIVE_TARGET=
RECOVERY_COMMIT=
TRUSTED_RELEASE_ROOT=
RELEASE_TRANSACTION_FD=
MUTATION_LOCK_FD=
recovery_snapshot_dir=
rescue_destination=

die() {
    printf '!! %s\n' "$*" >&2
    exit 1
}

validate_exact_systemctl() {
    local owner group mode links
    [[ $SYSTEMCTL_BIN == /* && -f $SYSTEMCTL_BIN && ! -L $SYSTEMCTL_BIN && -x $SYSTEMCTL_BIN ]] ||
        die "exact systemctl binary is unavailable or unsafe"
    read -r owner group mode links < <(/usr/bin/stat -Lc '%u %g %a %h' -- "$SYSTEMCTL_BIN") ||
        die "cannot inspect exact systemctl binary"
    [[ $owner == 0 && $group == 0 && $links == 1 ]] ||
        die "exact systemctl binary identity is unsafe"
    (( (8#$mode & 0022) == 0 )) ||
        die "exact systemctl binary is group/other writable"
}

systemctl() {
    "$SYSTEMCTL_BIN" "$@"
}

validate_exact_systemctl

usage() {
    printf '%s\n' \
        "usage: $0 --active-target=<40-lowercase-hex> --recovery-commit=<40-lowercase-hex> --trusted-release=<absolute-immutable-release-root>" >&2
    exit 2
}

for argument in "$@"; do
    case "$argument" in
        --active-target=*)
            [[ -z "$ACTIVE_TARGET" ]] || usage
            ACTIVE_TARGET=${argument#--active-target=}
            ;;
        --recovery-commit=*)
            [[ -z "$RECOVERY_COMMIT" ]] || usage
            RECOVERY_COMMIT=${argument#--recovery-commit=}
            ;;
        --trusted-release=*)
            [[ -z "$TRUSTED_RELEASE_ROOT" ]] || usage
            TRUSTED_RELEASE_ROOT=${argument#--trusted-release=}
            ;;
        *) usage ;;
    esac
done
[[ $# -eq 3 &&
   "$ACTIVE_TARGET" =~ ^[0-9a-f]{40}$ &&
   "$RECOVERY_COMMIT" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "$TRUSTED_RELEASE_ROOT" == /* ]] || usage
[[ $EUID -eq 0 ]] || die "recovery must run as root"

for required_command in \
    awk bash basename chmod chown cmp cp dirname find flock getent grep id install mkdir \
    mktemp mv readlink rm sha256sum sort stat sync systemctl tr xargs; do
    command -v "$required_command" >/dev/null 2>&1 \
        || die "required recovery tool is missing: $required_command"
done

validate_root_trusted_dir_chain() {
    local path=$1 canonical current owner mode permissions
    [[ "$path" == /* ]] || die "trusted directory path must be absolute: $path"
    canonical=$(readlink -e -- "$path") \
        || die "trusted directory is unavailable: $path"
    [[ "$canonical" == "$path" ]] \
        || die "trusted directory path contains a symlink or alias: $path"
    current=$path
    while true; do
        [[ -d "$current" && ! -L "$current" ]] \
            || die "unsafe trusted directory: $current"
        read -r owner mode < <(stat -Lc '%u %a' -- "$current") \
            || die "cannot inspect trusted directory: $current"
        [[ "$owner" == 0 ]] \
            || die "trusted directory must be owned by root: $current"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "trusted directory must not be group/other writable: $current"
        [[ "$current" == / ]] && break
        current=$(dirname -- "$current")
    done
}

ensure_recovery_snapshot_directory() {
    local parent canonical owner group mode
    parent=$(dirname -- "$RECOVERY_SNAPSHOT_ROOT")
    validate_root_trusted_dir_chain "$parent"
    if [[ ! -e "$RECOVERY_SNAPSHOT_ROOT" && ! -L "$RECOVERY_SNAPSHOT_ROOT" ]]; then
        install -d -o root -g root -m 0700 -- "$RECOVERY_SNAPSHOT_ROOT" \
            || die "cannot create the private recovery snapshot root"
        sync -f -- "$parent" \
            || die "cannot make the recovery snapshot root durable"
    fi
    [[ -d "$RECOVERY_SNAPSHOT_ROOT" && ! -L "$RECOVERY_SNAPSHOT_ROOT" ]] \
        || die "recovery snapshot root is not a safe directory"
    canonical=$(readlink -e -- "$RECOVERY_SNAPSHOT_ROOT") \
        || die "cannot resolve the recovery snapshot root"
    [[ "$canonical" == "$RECOVERY_SNAPSHOT_ROOT" ]] \
        || die "recovery snapshot root contains a symlink or alias"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$RECOVERY_SNAPSHOT_ROOT") \
        || die "cannot inspect the recovery snapshot root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "recovery snapshot root must be root:root mode 0700"

    recovery_snapshot_dir=$RECOVERY_SNAPSHOT_ROOT/$snapshot_name
    if [[ ! -e "$recovery_snapshot_dir" && ! -L "$recovery_snapshot_dir" ]]; then
        install -d -o root -g root -m 0700 -- "$recovery_snapshot_dir" \
            || die "cannot create the exact private recovery snapshot directory"
        sync -f -- "$RECOVERY_SNAPSHOT_ROOT" \
            || die "cannot make the recovery snapshot directory durable"
    fi
    [[ -d "$recovery_snapshot_dir" && ! -L "$recovery_snapshot_dir" ]] \
        || die "exact recovery snapshot path is not a safe directory"
    canonical=$(readlink -e -- "$recovery_snapshot_dir") \
        || die "cannot resolve the exact recovery snapshot directory"
    [[ "$canonical" == "$recovery_snapshot_dir" ]] \
        || die "exact recovery snapshot directory contains a symlink or alias"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$recovery_snapshot_dir") \
        || die "cannot inspect the exact recovery snapshot directory"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "exact recovery snapshot directory must be root:root mode 0700"
    rescue_destination=$recovery_snapshot_dir/$(basename -- "$PANEL_DB")
}

# The persistent release flock is acquired before any release-controlled code
# is trusted. An active recovery never creates missing coordination objects.
acquire_release_transaction_lock() {
    local parent lock owner group mode links size path_identity fd_identity
    parent=$(dirname -- "$TRANSACTION_ROOT")
    [[ "$parent" == /var/lib && -d "$parent" && ! -L "$parent" ]] \
        || die "unsafe release transaction parent"
    [[ "$(readlink -e -- "$parent")" == "$parent" ]] \
        || die "release transaction parent is not canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$parent") \
        || die "cannot inspect release transaction parent"
    [[ "$owner" == 0 && "$group" == 0 ]] \
        || die "release transaction parent must be root-owned"
    (( (8#$mode & 0022) == 0 )) \
        || die "release transaction parent must not be group/other writable"

    [[ -d "$TRANSACTION_ROOT" && ! -L "$TRANSACTION_ROOT" ]] \
        || die "active release transaction root is missing or unsafe"
    [[ "$(readlink -e -- "$TRANSACTION_ROOT")" == "$TRANSACTION_ROOT" ]] \
        || die "release transaction root is not canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$TRANSACTION_ROOT") \
        || die "cannot inspect release transaction root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "release transaction root must be root:root mode 0700"

    lock=$TRANSACTION_ROOT/transaction.lock
    [[ -f "$lock" && ! -L "$lock" ]] \
        || die "release transaction lock is missing or unsafe"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$lock") \
        || die "cannot inspect release transaction lock"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 &&
       "$links" == 1 && "$size" == 0 ]] \
        || die "release transaction lock must be empty root:root mode 0600 with one link"
    path_identity=$(stat -Lc '%d:%i' -- "$lock") \
        || die "cannot identify release transaction lock"
    exec {RELEASE_TRANSACTION_FD}<>"$lock"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$RELEASE_TRANSACTION_FD") \
        || die "cannot identify opened release transaction lock"
    [[ "$fd_identity" == "$path_identity" ]] \
        || die "release transaction lock changed while opening"
    flock -n -x "$RELEASE_TRANSACTION_FD" \
        || die "another update, rollback, or recovery owns the release transaction"
}

validate_trusted_release() {
    local canonical relative entry owner mode permissions version commit tree helper
    canonical=$(readlink -e -- "$TRUSTED_RELEASE_ROOT") \
        || die "trusted release root is unavailable"
    [[ "$canonical" == "$TRUSTED_RELEASE_ROOT" ]] \
        || die "trusted release root contains an alias"
    [[ "$canonical" == "$RELEASES_ROOT/"* ]] \
        || die "trusted release is outside release storage"
    relative=${canonical#"$RELEASES_ROOT/"}
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] \
        || die "trusted release must be a canonical direct child"
    [[ "${relative:0:12}" == "${RECOVERY_COMMIT:0:12}" ]] \
        || die "trusted release basename does not match --recovery-commit"
    validate_root_trusted_dir_chain "$canonical"
    read -r owner mode < <(stat -Lc '%u %a' -- "$canonical") \
        || die "cannot inspect trusted release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] \
        || die "trusted release root must be root-owned mode 0700"
    if find "$canonical" -type l -print -quit | grep -q .; then
        die "trusted release contains a symbolic link"
    fi
    if find "$canonical" ! -type d ! -type f -print -quit | grep -q .; then
        die "trusted release contains a special filesystem object"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") \
            || die "cannot inspect trusted release entry: $entry"
        [[ "$owner" == 0 ]] \
            || die "trusted release entry must be owned by root: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "trusted release entry must not be group/other writable: $entry"
    done < <(find "$canonical" -mindepth 1 -print0)
    [[ ! -e "$canonical/.git" && ! -L "$canonical/.git" ]] \
        || die "trusted release must be an immutable archive"
    [[ -f "$canonical/SHA256SUMS" && ! -L "$canonical/SHA256SUMS" ]] \
        || die "trusted release checksum manifest is missing"
    (
        cd "$canonical"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "trusted release checksum verification failed"
    [[ -f "$canonical/release.version" && ! -L "$canonical/release.version" &&
       -f "$canonical/release.commit" && ! -L "$canonical/release.commit" &&
       -f "$canonical/release.tree" && ! -L "$canonical/release.tree" ]] \
        || die "trusted release provenance is incomplete"
    version=$(tr -d '[:space:]' < "$canonical/release.version")
    commit=$(tr -d '[:space:]' < "$canonical/release.commit")
    tree=$(tr -d '[:space:]' < "$canonical/release.tree")
    [[ "$version" == 1 ]] || die "unsupported trusted release version: $version"
    [[ "$commit" == "$RECOVERY_COMMIT" ]] \
        || die "trusted recovery release commit does not equal --recovery-commit"
    [[ "$tree" =~ ^[0-9a-f]{40}$ || "$tree" =~ ^[0-9a-f]{64}$ ]] \
        || die "trusted recovery release tree is not a canonical object id"
    [[ -x "$canonical/bin/panel" && -f "$canonical/bin/panel" &&
       ! -L "$canonical/bin/panel" ]] \
        || die "trusted recovery panel binary is missing or unsafe"
    [[ -x "$canonical/bin/agent" && -f "$canonical/bin/agent" &&
       ! -L "$canonical/bin/agent" ]] \
        || die "trusted recovery agent binary is missing or unsafe"
    [[ -f "$canonical/deploy/release-transaction-guard.sh" &&
       ! -L "$canonical/deploy/release-transaction-guard.sh" ]] \
        || die "trusted release transaction guard is missing or unsafe"
    [[ -f "$canonical/deploy/release-transaction-start-guard.sh" &&
       ! -L "$canonical/deploy/release-transaction-start-guard.sh" ]] \
        || die "trusted release transaction start guard is missing or unsafe"
    helper=$(readlink -e -- "$0") || die "cannot resolve running recovery helper"
    [[ "$helper" == "$canonical/deploy/recover-active-update-database.sh" ]] \
        || die "recovery helper must execute from the verified trusted release"
}

service_cgroup_pids() {
    local unit=$1 control_group cgroup_root procs_file pid
    control_group=$(systemctl show --property=ControlGroup --value "$unit") \
        || die "cannot inspect control group for $unit"
    [[ -n "$control_group" ]] || return 0
    [[ "$control_group" == /system.slice/* && "$control_group" != *'/../'* ]] \
        || die "unsafe control group for $unit: $control_group"
    cgroup_root=/sys/fs/cgroup$control_group
    [[ -d "$cgroup_root" && ! -L "$cgroup_root" ]] || return 0
    find "$cgroup_root" -type f -name cgroup.procs -print0 \
        | while IFS= read -r -d '' procs_file; do
            while IFS= read -r pid; do
                [[ -z "$pid" || "$pid" =~ ^[0-9]+$ ]] \
                    || die "invalid pid in $procs_file"
                [[ -z "$pid" ]] || printf '%s\n' "$pid"
            done < "$procs_file"
        done
}

verify_unit_recursively_stopped() {
    local unit=$1 load state job main_pid pid_output
    load=$(systemctl show --property=LoadState --value "$unit") \
        || die "cannot inspect load state for $unit"
    [[ "$load" == loaded ]] || die "$unit must remain loaded during recovery"
    job=$(systemctl show --property=Job --value "$unit") \
        || die "cannot inspect queued job for $unit"
    [[ -z "$job" ]] || die "$unit has a queued systemd job: $job"
    state=$(systemctl show --property=ActiveState --value "$unit") \
        || die "cannot inspect active state for $unit"
    case "$state" in
        inactive|failed) ;;
        *) die "$unit must be inactive or failed; found $state" ;;
    esac
    main_pid=$(systemctl show --property=MainPID --value "$unit") \
        || die "cannot inspect MainPID for $unit"
    [[ "$main_pid" == 0 ]] || die "$unit MainPID must be zero; found $main_pid"
    pid_output=$(service_cgroup_pids "$unit") \
        || die "cannot recursively inspect cgroup processes for $unit"
    [[ -z "$pid_output" ]] \
        || die "$unit has residual recursive cgroup processes: ${pid_output//$'\n'/,}"
}

verify_both_units_stopped() {
    verify_unit_recursively_stopped celikpanel-agent.service
    verify_unit_recursively_stopped celikpanel-panel.service
}

install_and_verify_runtime_directory_preserve() {
    local directory target tmp owner group mode links loaded_dropins loaded found manager_value
    release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        || die "release transaction lock changed before runtime-directory preservation"
    validate_root_trusted_dir_chain "$UNIT_DIR"
    directory=$UNIT_DIR/celikpanel-agent.service.d
    if [[ -e "$directory" || -L "$directory" ]]; then
        [[ -d "$directory" && ! -L "$directory" ]] \
            || die "agent systemd drop-in directory is unsafe"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$directory") \
            || die "cannot inspect agent systemd drop-in directory"
        [[ "$owner" == 0 && "$group" == 0 && "$mode" == 755 ]] \
            || die "agent systemd drop-in directory must be root:root mode 0755"
    else
        install -d -m 0755 -o root -g root -- "$directory" \
            || die "cannot create agent systemd drop-in directory"
        sync -f -- "$UNIT_DIR" \
            || die "cannot make agent systemd drop-in directory durable"
    fi
    validate_root_trusted_dir_chain "$directory"
    target=$directory/09-runtime-directory-preserve.conf
    if [[ -e "$target" || -L "$target" ]]; then
        [[ -f "$target" && ! -L "$target" ]] \
            || die "existing runtime-directory preserve drop-in is unsafe"
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$target") \
            || die "cannot inspect existing runtime-directory preserve drop-in"
        [[ "$owner" == 0 && "$group" == 0 && "$mode" == 644 && "$links" == 1 ]] \
            || die "existing runtime-directory preserve drop-in metadata is unsafe"
    fi
    tmp=$(mktemp -p "$directory" '.09-runtime-directory-preserve.conf.tmp.XXXXXXXXXX') \
        || die "cannot stage runtime-directory preserve drop-in"
    if ! printf '[Service]\nRuntimeDirectoryPreserve=yes\n' > "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0644 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T -- "$tmp" "$target" ||
       ! sync -f -- "$directory"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        die "cannot durably install runtime-directory preserve drop-in"
    fi
    [[ -f "$target" && ! -L "$target" ]] \
        || die "installed runtime-directory preserve drop-in is unsafe"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$target") \
        || die "cannot inspect installed runtime-directory preserve drop-in"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 644 && "$links" == 1 ]] \
        || die "installed runtime-directory preserve drop-in metadata is unsafe"
    cmp -s "$target" <(printf '[Service]\nRuntimeDirectoryPreserve=yes\n') \
        || die "installed runtime-directory preserve drop-in content is invalid"
    "$SYSTEMCTL_BIN" daemon-reload \
        || die "systemd daemon-reload failed after runtime-directory preservation"
    loaded_dropins=$("$SYSTEMCTL_BIN" show --property=DropInPaths --value celikpanel-agent.service) \
        || die "cannot inspect loaded agent systemd drop-ins"
    found=0
    for loaded in $loaded_dropins; do
        if [[ "$loaded" == "$target" ]]; then
            found=1
            break
        fi
    done
    [[ "$found" == 1 ]] \
        || die "systemd did not load the exact runtime-directory preserve drop-in"
    manager_value=$("$SYSTEMCTL_BIN" show --property=RuntimeDirectoryPreserve --value celikpanel-agent.service) \
        || die "cannot inspect loaded RuntimeDirectoryPreserve value"
    [[ "$manager_value" == yes ]] \
        || die "systemd did not load RuntimeDirectoryPreserve=yes for the agent"
}

# systemd removes the agent RuntimeDirectory after both coordinators stop.
# Recreate only the fixed shared-flock directory during an already-proven active
# recovery; every existing object and the result are validated fail-closed.
prepare_runtime_mutation_lock_dir() {
    local lock_dir parent expected_group owner group mode links size entry
    local dotglob_was_set=0 nullglob_was_set=0
    local -a entries=()
    lock_dir=$(dirname -- "$MUTATION_LOCK")
    [[ "$lock_dir" == /run/celikpanel ]] \
        || die "unexpected mutation lock directory: $lock_dir"
    parent=$(dirname -- "$lock_dir")
    [[ "$parent" == /run && -d "$parent" && ! -L "$parent" ]] \
        || die "unsafe mutation lock parent: $parent"
    validate_root_trusted_dir_chain "$parent"
    expected_group=$(getent group celikpanel | awk -F: 'NR == 1 { print $3 }') \
        || die "celikpanel group is unavailable"
    [[ "$expected_group" =~ ^[0-9]+$ && "$expected_group" -gt 0 ]] \
        || die "celikpanel group id is invalid"
    if [[ -e "$lock_dir" || -L "$lock_dir" ]]; then
        [[ -d "$lock_dir" && ! -L "$lock_dir" ]] \
            || die "mutation lock directory exists but is unsafe"
        validate_root_trusted_dir_chain "$lock_dir"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
            || die "cannot inspect the existing mutation lock directory"
        [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 750 ]] \
            || die "existing mutation lock directory must be root:celikpanel mode 0750"
    else
        install -d -m 0750 -o root -g celikpanel -- "$lock_dir" \
            || die "cannot prepare the ephemeral mutation lock directory"
    fi
    validate_root_trusted_dir_chain "$lock_dir"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect the prepared mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
    shopt -q dotglob && dotglob_was_set=1
    shopt -q nullglob && nullglob_was_set=1
    shopt -s dotglob nullglob
    entries=("$lock_dir"/*)
    (( dotglob_was_set == 1 )) || shopt -u dotglob
    (( nullglob_was_set == 1 )) || shopt -u nullglob
    (( ${#entries[@]} <= 1 )) \
        || die "mutation lock directory contains unexpected entries"
    if (( ${#entries[@]} == 1 )); then
        entry=${entries[0]}
        [[ "$entry" == "$MUTATION_LOCK" ]] \
            || die "mutation lock directory contains an unexpected entry: $entry"
        [[ -f "$entry" && ! -L "$entry" ]] \
            || die "existing mutation lock is not a safe regular file"
        read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$entry") \
            || die "cannot inspect the existing mutation lock"
        [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 600 &&
           "$links" == 1 && "$size" == 0 ]] \
            || die "existing mutation lock must be empty root:celikpanel mode 0600 with one link"
    fi
}

acquire_mutation_lock() {
    local lock_dir expected_group owner group mode links size path_identity fd_identity
    expected_group=$(getent group celikpanel | awk -F: 'NR == 1 { print $3 }') \
        || die "celikpanel group is unavailable"
    [[ "$expected_group" =~ ^[0-9]+$ && "$expected_group" -gt 0 ]] \
        || die "celikpanel group id is invalid"
    lock_dir=$(dirname -- "$MUTATION_LOCK")
    validate_root_trusted_dir_chain "$lock_dir"
    [[ -d "$lock_dir" && ! -L "$lock_dir" ]] \
        || die "mutation lock directory is missing or unsafe"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
    if [[ -e "$MUTATION_LOCK" || -L "$MUTATION_LOCK" ]]; then
        [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] \
            || die "mutation lock exists but is unsafe"
    else
        (umask 077; set -o noclobber; : > "$MUTATION_LOCK") \
            || die "cannot exclusively create the ephemeral mutation lock"
        chown root:celikpanel -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock ownership"
        chmod 0600 -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock permissions"
    fi
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$MUTATION_LOCK") \
        || die "cannot inspect mutation lock"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 600 &&
       "$links" == 1 && "$size" == 0 ]] \
        || die "mutation lock must be empty root:celikpanel mode 0600 with one link"
    path_identity=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") \
        || die "cannot identify mutation lock"
    exec {MUTATION_LOCK_FD}<>"$MUTATION_LOCK"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify opened mutation lock"
    [[ "$fd_identity" == "$path_identity" ]] \
        || die "mutation lock changed while opening"
    flock -n -x "$MUTATION_LOCK_FD" \
        || die "a service or package mutation is active"
}

verify_mutation_lock_held() {
    local lock_dir expected_group owner group mode links size path_identity fd_identity
    local dotglob_was_set=0 nullglob_was_set=0
    local -a entries=()
    [[ "$MUTATION_LOCK_FD" =~ ^[0-9]+$ ]] \
        || die "mutation lock descriptor is invalid"
    [[ -e "/proc/$BASHPID/fd/$MUTATION_LOCK_FD" ]] \
        || die "mutation lock descriptor is no longer open"
    expected_group=$(getent group celikpanel | awk -F: 'NR == 1 { print $3 }') \
        || die "celikpanel group is unavailable"
    [[ "$expected_group" =~ ^[0-9]+$ && "$expected_group" -gt 0 ]] \
        || die "celikpanel group id is invalid"
    lock_dir=$(dirname -- "$MUTATION_LOCK")
    [[ -d "$lock_dir" && ! -L "$lock_dir" ]] \
        || die "mutation lock directory is missing or unsafe"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 750 ]] \
        || die "mutation lock directory must remain root:celikpanel mode 0750"
    shopt -q dotglob && dotglob_was_set=1
    shopt -q nullglob && nullglob_was_set=1
    shopt -s dotglob nullglob
    entries=("$lock_dir"/*)
    (( dotglob_was_set == 1 )) || shopt -u dotglob
    (( nullglob_was_set == 1 )) || shopt -u nullglob
    (( ${#entries[@]} == 1 )) \
        || die "held mutation lock directory must contain exactly the fixed lock"
    [[ "${entries[0]}" == "$MUTATION_LOCK" ]] \
        || die "held mutation lock directory contains an unexpected entry"
    [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] \
        || die "mutation lock pathname is missing or unsafe"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$MUTATION_LOCK") \
        || die "cannot inspect held mutation lock"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 600 &&
       "$links" == 1 && "$size" == 0 ]] \
        || die "held mutation lock must remain empty root:celikpanel mode 0600 with one link"
    path_identity=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") \
        || die "cannot identify mutation lock pathname"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify held mutation lock descriptor"
    [[ "$fd_identity" == "$path_identity" ]] \
        || die "mutation lock pathname no longer names the held lock inode"
    flock -n -x "$MUTATION_LOCK_FD" \
        || die "mutation flock ownership changed"
}

verify_source_database_precondition() {
    local expected_uid expected_gid owner group mode links
    expected_uid=$(id -u celikpanel) || die "celikpanel user is unavailable"
    expected_gid=$(getent group celikpanel | awk -F: 'NR == 1 { print $3 }') \
        || die "celikpanel group is unavailable"
    [[ "$expected_uid" =~ ^[0-9]+$ && "$expected_uid" -gt 0 &&
       "$expected_gid" =~ ^[0-9]+$ && "$expected_gid" -gt 0 ]] \
        || die "celikpanel service identity is invalid"
    [[ -f "$PANEL_DB" && ! -L "$PANEL_DB" ]] \
        || die "canonical panel database is missing or unsafe"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$PANEL_DB") \
        || die "cannot inspect canonical panel database"
    [[ "$owner" == "$expected_uid" && "$group" == "$expected_gid" && "$links" == 1 ]] \
        || die "canonical panel database must already be celikpanel-owned with one link"
    [[ "$mode" == 600 || "$mode" == 640 || "$mode" == 644 ]] \
        || die "canonical panel database mode must be 0600 or the exact legacy 0640/0644"
}

verify_root_snapshot_file() {
    local path=$1 owner group mode links
    [[ -f "$path" && ! -L "$path" ]] \
        || die "recovery snapshot was not created as a safe regular file"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") \
        || die "cannot inspect recovery snapshot"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 ]] \
        || die "recovery snapshot must be root:root mode 0600 with one link"
}

verify_canonical_database_postcondition() {
    local expected_uid expected_gid owner group mode links
    expected_uid=$(id -u celikpanel) || die "celikpanel user is unavailable"
    expected_gid=$(getent group celikpanel | awk -F: 'NR == 1 { print $3 }') \
        || die "celikpanel group is unavailable"
    [[ -f "$PANEL_DB" && ! -L "$PANEL_DB" ]] \
        || die "canonical panel database became unsafe"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$PANEL_DB") \
        || die "cannot inspect normalized canonical panel database"
    [[ "$owner" == "$expected_uid" && "$group" == "$expected_gid" &&
       "$mode" == 600 && "$links" == 1 ]] \
        || die "canonical panel database was not normalized to celikpanel-owned mode 0600 with one link"
}

require_no_sqlite_sidecars() {
    local path=$1 suffix sidecar
    for suffix in -wal -shm -journal; do
        sidecar=$path$suffix
        [[ ! -e "$sidecar" && ! -L "$sidecar" ]] \
            || die "SQLite sidecar remains after trusted recovery: $sidecar"
    done
}

run_trusted_agent_idle_check() {
    env -i \
        PATH="$PATH" HOME=/root LC_ALL=C \
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \
        CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$TRUSTED_RELEASE_ROOT/bin/agent" \
        --check-service-mutation-idle-under-external-lock \
        || die "trusted recovery agent did not prove an idle mutation ledger"
}

run_trusted_panel_snapshot_operation() {
    local operation=$1 destination=$2
    env -i \
        PATH="$PATH" HOME=/root LC_ALL=C \
        CELIKPANEL_DATA_DIR="$PANEL_DATA_DIR" \
        "$TRUSTED_RELEASE_ROOT/bin/panel" \
        "$operation=$destination" \
        --snapshot-schema=normal \
        --release-transaction-fd="$RELEASE_TRANSACTION_FD" \
        --release-transaction-token="$transaction_token" \
        --release-transaction-operation=update \
        --release-transaction-snapshot="$snapshot_name"
}

acquire_release_transaction_lock
validate_trusted_release

# shellcheck source=deploy/release-transaction-guard.sh
source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"
release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "trusted guard rejected the inherited release transaction lock"

[[ ! -e "$TRANSACTION_ROOT/quiesce.pending" &&
   ! -L "$TRANSACTION_ROOT/quiesce.pending" &&
   ! -e "$TRANSACTION_ROOT/completion.pending" &&
   ! -L "$TRANSACTION_ROOT/completion.pending" ]] \
    || die "recovery requires the active marker to be the only durable phase"

IFS=$'\t' read -r transaction_token transaction_operation snapshot_name \
    < <(release_txn_read_active_fields "$TRANSACTION_ROOT") \
    || die "cannot read the exact active update marker"
[[ "$transaction_operation" == update ]] \
    || die "active transaction is not an update"
IFS=$'\t' read -r snapshot_created marker_target snapshot_nonce \
    < <(release_txn_parse_update_snapshot_name "$snapshot_name") \
    || die "active update snapshot name is not canonical"
[[ -n "$snapshot_created" && -n "$snapshot_nonce" &&
   "$marker_target" == "$ACTIVE_TARGET" ]] \
    || die "active update target does not equal --active-target"
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active update marker failed exact validation"

active_marker=$TRANSACTION_ROOT/active
active_marker_identity=$(stat -Lc '%d:%i' -- "$active_marker") \
    || die "cannot identify active update marker"
active_marker_digest=$(sha256sum "$active_marker" | awk '{ print $1 }') \
    || die "cannot hash active update marker"

[[ ! -e "$SNAPSHOT_ROOT/$snapshot_name" &&
   ! -L "$SNAPSHOT_ROOT/$snapshot_name" ]] \
    || die "final snapshot already exists; recovery refuses to overwrite or advance it"
stage_root=$(release_txn_find_update_snapshot_stage "$SNAPSHOT_ROOT" "$snapshot_name") \
    || die "cannot find exactly one canonical active-update stage"
snapshot_child=$stage_root/$snapshot_name
printf 'normal\n' | cmp -s - "$snapshot_child/snapshot-transition.state" \
    || die "database recovery supports only a normal update stage"
snapshot_destination=$snapshot_child/$(basename -- "$PANEL_DB")

verify_source_database_precondition
verify_both_units_stopped
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active marker changed before recovery guard installation"
[[ "$(stat -Lc '%d:%i' -- "$active_marker")" == "$active_marker_identity" ]] \
    || die "active marker identity changed before recovery guard installation"
[[ "$(sha256sum "$active_marker" | awk '{ print $1 }')" == "$active_marker_digest" ]] \
    || die "active marker bytes changed before recovery guard installation"
release_txn_validate_update_snapshot_stage \
    "$SNAPSHOT_ROOT" "$snapshot_name" "$stage_root" \
    || die "snapshot stage changed before recovery guard installation"

install_and_verify_runtime_directory_preserve
release_txn_install_and_verify_unit_guards \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$RELEASE_TRANSACTION_HELPER" "$RELEASE_TRANSACTION_FD" "$SYSTEMCTL_BIN" \
    "" "$UNIT_DIR/celikpanel-agent.service.d/09-runtime-directory-preserve.conf" \
    || die "release transaction service guards could not be installed and verified"
release_txn_clear_stale_start_authorization \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "stale release start authorization could not be cleared"
verify_both_units_stopped
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active marker changed after recovery guard installation"
[[ "$(stat -Lc '%d:%i' -- "$active_marker")" == "$active_marker_identity" ]] \
    || die "active marker identity changed after recovery guard installation"
[[ "$(sha256sum "$active_marker" | awk '{ print $1 }')" == "$active_marker_digest" ]] \
    || die "active marker bytes changed after recovery guard installation"
release_txn_validate_update_snapshot_stage \
    "$SNAPSHOT_ROOT" "$snapshot_name" "$stage_root" \
    || die "snapshot stage changed after recovery guard installation"

prepare_runtime_mutation_lock_dir

# A coordinator that held the old /run/celikpanel lock inode before it stopped
# may still have caused that pathname to be unlinked during RuntimeDirectory
# teardown. RuntimeDirectoryPreserve=yes, start guards, empty queued jobs and
# empty recursive cgroups close that old process domain before the recreated
# pathname can become authoritative. The new flock is accepted only after the
# trusted agent also proves its durable mutation ledger and package manager idle.
install_and_verify_runtime_directory_preserve
release_txn_install_and_verify_unit_guards \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$RELEASE_TRANSACTION_HELPER" "$RELEASE_TRANSACTION_FD" "$SYSTEMCTL_BIN" \
    "" "$UNIT_DIR/celikpanel-agent.service.d/09-runtime-directory-preserve.conf" \
    || die "release transaction service guards changed after runtime preparation"
release_txn_clear_stale_start_authorization \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "stale release start authorization reappeared after runtime preparation"
verify_both_units_stopped
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active marker changed while preparing the ephemeral mutation lock"
[[ "$(stat -Lc '%d:%i' -- "$active_marker")" == "$active_marker_identity" ]] \
    || die "active marker identity changed while preparing the ephemeral mutation lock"
[[ "$(sha256sum "$active_marker" | awk '{ print $1 }')" == "$active_marker_digest" ]] \
    || die "active marker bytes changed while preparing the ephemeral mutation lock"
release_txn_validate_update_snapshot_stage \
    "$SNAPSHOT_ROOT" "$snapshot_name" "$stage_root" \
    || die "snapshot stage changed while preparing the ephemeral mutation lock"
acquire_mutation_lock
verify_mutation_lock_held
verify_both_units_stopped
run_trusted_agent_idle_check
verify_mutation_lock_held
verify_both_units_stopped
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active marker changed before database recovery"
[[ "$(stat -Lc '%d:%i' -- "$active_marker")" == "$active_marker_identity" ]] \
    || die "active marker identity changed before database recovery"
[[ "$(sha256sum "$active_marker" | awk '{ print $1 }')" == "$active_marker_digest" ]] \
    || die "active marker bytes changed before database recovery"
release_txn_validate_update_snapshot_stage \
    "$SNAPSHOT_ROOT" "$snapshot_name" "$stage_root" \
    || die "snapshot stage changed before database recovery"

ensure_recovery_snapshot_directory
verify_mutation_lock_held
run_trusted_panel_snapshot_operation \
    --ensure-service-operation-rescue-snapshot "$rescue_destination" \
    || die "trusted recovery panel could not ensure the durable rescue snapshot at $rescue_destination"
verify_mutation_lock_held

release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "release transaction lock changed during rescue snapshot creation"
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active marker changed during rescue snapshot creation"
[[ "$(stat -Lc '%d:%i' -- "$active_marker")" == "$active_marker_identity" ]] \
    || die "active marker identity changed during rescue snapshot creation"
[[ "$(sha256sum "$active_marker" | awk '{ print $1 }')" == "$active_marker_digest" ]] \
    || die "active marker bytes changed during rescue snapshot creation"
[[ ! -e "$SNAPSHOT_ROOT/$snapshot_name" &&
   ! -L "$SNAPSHOT_ROOT/$snapshot_name" ]] \
    || die "rescue recovery unexpectedly published a final snapshot"
release_txn_validate_update_snapshot_stage \
    "$SNAPSHOT_ROOT" "$snapshot_name" "$stage_root" \
    || die "snapshot stage changed during rescue snapshot creation"
verify_both_units_stopped
run_trusted_agent_idle_check
verify_mutation_lock_held
verify_both_units_stopped
verify_root_snapshot_file "$rescue_destination"
require_no_sqlite_sidecars "$rescue_destination"
verify_source_database_precondition
sync -f -- "$rescue_destination" "$recovery_snapshot_dir" \
    || die "durable rescue snapshot could not be synchronized"

[[ ! -e "$snapshot_destination" && ! -L "$snapshot_destination" ]] \
    || die "stage snapshot already exists; fail-closed with the rescue preserved at $rescue_destination"

verify_mutation_lock_held
run_trusted_panel_snapshot_operation \
    --create-service-operation-snapshot "$snapshot_destination" \
    || die "stage snapshot or canonical normalization failed; rescue remains at $rescue_destination"
verify_mutation_lock_held

release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "release transaction lock changed during database recovery"
release_txn_validate_active_token \
    "$TRANSACTION_ROOT" "$transaction_token" update "$snapshot_name" \
    || die "active marker changed during database recovery"
[[ "$(stat -Lc '%d:%i' -- "$active_marker")" == "$active_marker_identity" ]] \
    || die "active marker identity changed during database recovery"
[[ "$(sha256sum "$active_marker" | awk '{ print $1 }')" == "$active_marker_digest" ]] \
    || die "active marker bytes changed during database recovery"
[[ ! -e "$SNAPSHOT_ROOT/$snapshot_name" &&
   ! -L "$SNAPSHOT_ROOT/$snapshot_name" ]] \
    || die "recovery unexpectedly published a final snapshot"
release_txn_validate_update_snapshot_stage \
    "$SNAPSHOT_ROOT" "$snapshot_name" "$stage_root" \
    || die "snapshot stage changed during database recovery"
verify_both_units_stopped
run_trusted_agent_idle_check
verify_mutation_lock_held
verify_root_snapshot_file "$rescue_destination"
require_no_sqlite_sidecars "$rescue_destination"
verify_root_snapshot_file "$snapshot_destination"
require_no_sqlite_sidecars "$snapshot_destination"
verify_canonical_database_postcondition
require_no_sqlite_sidecars "$PANEL_DB"
sync -f -- "$snapshot_destination" "$snapshot_child" \
    || die "recovery snapshot could not be made durable"

printf '\n%s\n' "==> Database recovery snapshot is ready; the active transaction remains preserved."
printf '%s\n' "    Active target: $ACTIVE_TARGET"
printf '%s\n' "    Recovery binary commit: $RECOVERY_COMMIT"
printf '%s\n' "    Snapshot: $snapshot_name"
printf '%s\n' "    Stage: $stage_root"
printf '%s\n' "    Durable rescue: $rescue_destination"
printf '%s\n' "    Both CelikPanel coordinators remain stopped."
printf '%s\n' "==> Do not run the recovery release updater."
printf '%s\n' "==> Rerun the exact already-verified updater for active target $ACTIVE_TARGET"
printf '%s\n' "    with its original immutable release path and original --normal invocation."
