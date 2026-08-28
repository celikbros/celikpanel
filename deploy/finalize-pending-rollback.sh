#!/usr/bin/env bash
# Finalize only the guarded start/cleanup phase of one already-restored normal
# rollback. The recovery release supplies reviewed guard logic; the original
# transaction release supplies the exact checkers and provenance that created
# the snapshot. Installed bytes are proved against that snapshot, never against
# the newer recovery release or the transaction release.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

SNAPSHOT_VERSION=6
RELEASES_ROOT=/var/backups/celikpanel/releases
SNAPSHOT_ROOT=/var/backups/celikpanel/update-snapshots
TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
TRANSACTION_START_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
UNIT_DIR=/etc/systemd/system
SYSTEMCTL_BIN=/usr/bin/systemctl
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
AGENT_SOCKET=/run/celikpanel/agent.sock
AGENT_STATE_DIR=/var/lib/celikpanel-agent-private
PANEL_DATA_DIR=/var/lib/celikpanel
PANEL_DB=$PANEL_DATA_DIR/celikpanel.db
BIN_DIR=/opt/celikpanel/bin
WEB_DIR=/opt/celikpanel/web
RELEASE_UPDATER=/usr/libexec/celikpanel/get.sh
AGENT_LEDGER=$AGENT_STATE_DIR/service-mutations.json
PANEL_TLS_DIR=/var/lib/celikpanel/tls
PANEL_CERT_PENDING=$AGENT_STATE_DIR/panel-certificate-activation.json
PANEL_CERT_HOOK=/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert

PENDING_TARGET=
RECOVERY_COMMIT=
TRUSTED_RECOVERY_RELEASE=
TRUSTED_TRANSACTION_RELEASE=
TRUSTED_RELEASE_ROOT=
TRANSACTION_RELEASE_TREE=
RELEASE_TRANSACTION_FD=
MUTATION_LOCK_FD=
MUTATION_LOCK_IDENTITY=
pending_token=
pending_snapshot=
pending_snapshot_path=
firewall_state=
release_updater_state=
start_authorization_created=0
completion_verified=0
completion_removing=0
scheduler_restore_pending=0
scheduler_only_resume=0
scheduler_recovery_verified=0
scheduler_restore_completed=0
finalization_succeeded=0

declare -A saved_enabled_states=()
declare -A saved_active_states=()
declare -A quiesce_active_states=()
declare -A quiesce_main_pids=()
declare -A quiesce_start_times=()

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
        "usage: $0 --pending-target=<40-lowercase-hex> --recovery-commit=<40-lowercase-hex> --trusted-recovery-release=<absolute-immutable-release-root> --trusted-transaction-release=<absolute-immutable-release-root>" >&2
    exit 2
}

for argument in "$@"; do
    case "$argument" in
        --pending-target=*)
            [[ -z "$PENDING_TARGET" ]] || usage
            PENDING_TARGET=${argument#--pending-target=}
            ;;
        --recovery-commit=*)
            [[ -z "$RECOVERY_COMMIT" ]] || usage
            RECOVERY_COMMIT=${argument#--recovery-commit=}
            ;;
        --trusted-recovery-release=*)
            [[ -z "$TRUSTED_RECOVERY_RELEASE" ]] || usage
            TRUSTED_RECOVERY_RELEASE=${argument#--trusted-recovery-release=}
            ;;
        --trusted-transaction-release=*)
            [[ -z "$TRUSTED_TRANSACTION_RELEASE" ]] || usage
            TRUSTED_TRANSACTION_RELEASE=${argument#--trusted-transaction-release=}
            ;;
        *) usage ;;
    esac
done
[[ $# -eq 4 &&
   "$PENDING_TARGET" =~ ^[0-9a-f]{40}$ &&
   "$RECOVERY_COMMIT" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "$TRUSTED_RECOVERY_RELEASE" == /* &&
   "$TRUSTED_TRANSACTION_RELEASE" == /* ]] || usage
[[ "$RECOVERY_COMMIT" != "$PENDING_TARGET" &&
   "$TRUSTED_RECOVERY_RELEASE" != "$TRUSTED_TRANSACTION_RELEASE" ]] \
    || die "recovery and rollback-transaction provenance must be distinct"
[[ $EUID -eq 0 ]] || die "pending-rollback finalization must run as root"

for required_command in \
    awk bash chmod chown cmp cut dirname env find flock getent grep id install \
    mkdir mktemp mv readlink rm rmdir seq sha256sum sleep sort stat sudo sync \
    systemctl tr xargs; do
    command -v "$required_command" >/dev/null 2>&1 \
        || die "required finalization tool is missing: $required_command"
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
        || die "release transaction root is missing or unsafe"
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

validate_immutable_release() {
    local role=$1 root=$2 expected_commit=$3 required_helper=$4
    local canonical relative entry owner group mode links permissions version commit tree helper
    canonical=$(readlink -e -- "$root") \
        || die "$role release root is unavailable"
    [[ "$canonical" == "$root" ]] \
        || die "$role release root contains an alias"
    [[ "$canonical" == "$RELEASES_ROOT/"* ]] \
        || die "$role release is outside release storage"
    relative=${canonical#"$RELEASES_ROOT/"}
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] \
        || die "$role release must be a canonical direct child"
    [[ "${relative:0:12}" == "${expected_commit:0:12}" ]] \
        || die "$role release basename does not match its expected commit"
    validate_root_trusted_dir_chain "$canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$canonical") \
        || die "cannot inspect $role release root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "$role release root must be root:root mode 0700"
    if find "$canonical" -type l -print -quit | grep -q .; then
        die "$role release contains a symbolic link"
    fi
    if find "$canonical" ! -type d ! -type f -print -quit | grep -q .; then
        die "$role release contains a special filesystem object"
    fi
    if find "$canonical" -type f -links +1 -print -quit | grep -q .; then
        die "$role release contains a multiply-linked regular file"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$entry") \
            || die "cannot inspect $role release entry: $entry"
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "$role release entry must be owned by root:root: $entry"
        if [[ -f "$entry" ]]; then
            [[ "$links" == 1 ]] \
                || die "$role release regular file must have one link: $entry"
        fi
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "$role release entry must not be group/other writable: $entry"
    done < <(find "$canonical" -mindepth 1 -print0)
    [[ ! -e "$canonical/.git" && ! -L "$canonical/.git" ]] \
        || die "$role release must be an immutable archive"
    [[ -f "$canonical/SHA256SUMS" && ! -L "$canonical/SHA256SUMS" ]] \
        || die "$role release checksum manifest is missing"
    (
        cd "$canonical"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "$role release checksum verification failed"
    [[ -f "$canonical/release.version" && ! -L "$canonical/release.version" &&
       -f "$canonical/release.commit" && ! -L "$canonical/release.commit" &&
       -f "$canonical/release.tree" && ! -L "$canonical/release.tree" ]] \
        || die "$role release provenance is incomplete"
    version=$(tr -d '[:space:]' < "$canonical/release.version")
    commit=$(tr -d '[:space:]' < "$canonical/release.commit")
    tree=$(tr -d '[:space:]' < "$canonical/release.tree")
    [[ "$version" == 1 ]] || die "unsupported $role release version: $version"
    [[ "$commit" == "$expected_commit" ]] \
        || die "$role release commit does not equal its explicit commit"
    [[ "$tree" =~ ^[0-9a-f]{40}$ || "$tree" =~ ^[0-9a-f]{64}$ ]] \
        || die "$role release tree is not a canonical object id"
    [[ -x "$canonical/bin/panel" && -f "$canonical/bin/panel" &&
       ! -L "$canonical/bin/panel" ]] \
        || die "$role panel binary is missing or unsafe"
    [[ -x "$canonical/bin/agent" && -f "$canonical/bin/agent" &&
       ! -L "$canonical/bin/agent" ]] \
        || die "$role agent binary is missing or unsafe"
    if [[ "$role" == recovery ]]; then
        [[ -f "$canonical/deploy/release-transaction-guard.sh" &&
           ! -L "$canonical/deploy/release-transaction-guard.sh" ]] \
            || die "recovery release transaction guard is missing or unsafe"
        [[ -f "$canonical/deploy/release-transaction-start-guard.sh" &&
           ! -L "$canonical/deploy/release-transaction-start-guard.sh" ]] \
            || die "recovery release transaction start guard is missing or unsafe"
        [[ -f "$canonical/deploy/panel-tls-snapshot.sh" &&
           ! -L "$canonical/deploy/panel-tls-snapshot.sh" ]] \
            || die "recovery release panel TLS snapshot helper is missing or unsafe"
        [[ -f "$canonical/deploy/finalize-pending-rollback.sh" &&
           ! -L "$canonical/deploy/finalize-pending-rollback.sh" ]] \
            || die "recovery release pending-rollback finalizer is missing or unsafe"
        helper=$(readlink -e -- "$0") \
            || die "cannot resolve running pending-rollback finalizer"
        [[ "$helper" == "$canonical/$required_helper" ]] \
            || die "pending-rollback finalizer must execute from the verified recovery release"
    else
        [[ -f "$canonical/web/dist/index.html" &&
           ! -L "$canonical/web/dist/index.html" ]] \
            || die "transaction web artifact is missing or unsafe"
        TRANSACTION_RELEASE_TREE=$tree
    fi
}

validate_binary() {
    local path=$1 label=$2 owner mode permissions
    [[ "$path" == /* ]] || die "$label path must be absolute"
    validate_root_trusted_dir_chain "$(dirname -- "$path")"
    [[ -f "$path" && -x "$path" && ! -L "$path" ]] \
        || die "$label is missing or unsafe"
    read -r owner mode < <(stat -Lc '%u %a' -- "$path") \
        || die "cannot inspect $label"
    [[ "$owner" == 0 ]] || die "$label must be owned by root"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) \
        || die "$label must not be group/other writable"
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
    [[ "$load" == loaded ]] || die "$unit must remain loaded"
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
    [[ "$main_pid" == 0 ]] || die "$unit MainPID must be zero"
    pid_output=$(service_cgroup_pids "$unit") \
        || die "cannot recursively inspect cgroup processes for $unit"
    [[ -z "$pid_output" ]] \
        || die "$unit has residual recursive cgroup processes: ${pid_output//$'\n'/,}"
}

verify_both_units_stopped() {
    verify_unit_recursively_stopped celikpanel-agent.service
    verify_unit_recursively_stopped celikpanel-panel.service
}

validate_service_active_state() {
    local unit=$1 state=$2
    case "$state" in
        active|activating|reloading|refreshing|inactive|failed|deactivating|maintenance) ;;
        *) die "unsupported active state for $unit: $state" ;;
    esac
}

service_state_is_active_like() {
    case "$1" in
        active|activating|reloading|refreshing) return 0 ;;
        *) return 1 ;;
    esac
}

coordinator_process_start_time() {
    local pid=$1 process_stat process_tail
    local -a process_fields=()
    [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 1 && -r "/proc/$pid/stat" ]] || return 1
    process_stat=$(<"/proc/$pid/stat") || return 1
    process_tail=${process_stat##*) }
    read -r -a process_fields <<< "$process_tail"
    [[ ${#process_fields[@]} -ge 20 && "${process_fields[19]}" =~ ^[0-9]+$ ]] || return 1
    printf '%s\n' "${process_fields[19]}"
}

load_saved_service_states() {
    local ledger=$1 unit enabled_state active_state extra count=0 expected_unit
    saved_enabled_states=()
    saved_active_states=()
    [[ -f "$ledger" && ! -L "$ledger" ]] \
        || die "service state ledger is missing or unsafe: $ledger"
    while IFS=$'\t' read -r unit enabled_state active_state extra ||
          [[ -n "$unit$enabled_state$active_state${extra:-}" ]]; do
        [[ -n "$unit" && -n "$enabled_state" && -n "$active_state" && -z "${extra:-}" ]] \
            || die "malformed service state ledger"
        case "$count" in
            0) expected_unit=celikpanel-agent.service ;;
            1) expected_unit=celikpanel-panel.service ;;
            2) expected_unit=celikpanel-firewall-restore.service ;;
            3) expected_unit=certbot.timer ;;
            4) expected_unit=certbot-renew.timer ;;
            *) die "service state ledger contains extra rows" ;;
        esac
        [[ "$unit" == "$expected_unit" ]] \
            || die "service state ledger order is not canonical: got $unit, want $expected_unit"
        if [[ "$count" -lt 3 ]]; then
            case "$enabled_state" in
                enabled|enabled-runtime|disabled|static|indirect|not-found) ;;
                *) die "unsupported saved enable state for $unit: $enabled_state" ;;
            esac
            validate_service_active_state "$unit" "$active_state"
            saved_enabled_states["$unit"]=$enabled_state
            saved_active_states["$unit"]=$active_state
        else
            case "$enabled_state" in
                enabled|enabled-runtime|linked|linked-runtime|alias|static|indirect|generated|disabled|masked|masked-runtime|not-found) ;;
                *) die "unsupported saved scheduler enable state for $unit: $enabled_state" ;;
            esac
            case "$active_state" in
                active|inactive) ;;
                *) die "unsupported saved scheduler active state for $unit: $active_state" ;;
            esac
        fi
        count=$((count + 1))
    done < "$ledger"
    [[ "$count" -eq 5 ]] \
        || die "service state ledger must contain exactly five canonical rows"
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        [[ -n "${saved_enabled_states[$unit]:-}" ]] \
            || die "service state is missing for $unit"
    done
    if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}" &&
       ! service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
        die "saved runtime state is inconsistent: an active panel requires an active agent"
    fi
}

load_quiesce_coordinator_identities() {
    local ledger=$1 unit state main_pid start_time extra count=0 expected_unit current_start
    quiesce_active_states=()
    quiesce_main_pids=()
    quiesce_start_times=()
    while IFS=$'\t' read -r unit state main_pid start_time extra; do
        [[ -n "$unit" && -n "$state" && -n "$main_pid" && -n "$start_time" &&
           -z "${extra:-}" ]] || die "malformed quiesce coordinator ledger"
        [[ "$count" -eq 0 ]] && expected_unit=celikpanel-agent.service \
            || expected_unit=celikpanel-panel.service
        [[ "$unit" == "$expected_unit" ]] \
            || die "quiesce coordinator ledger order is not canonical"
        [[ -z "${quiesce_active_states[$unit]+x}" ]] \
            || die "duplicate quiesce coordinator row: $unit"
        validate_service_active_state "$unit" "$state"
        [[ "$state" == "${saved_active_states[$unit]:-}" ]] \
            || die "quiesce coordinator state differs from service ledger: $unit"
        if service_state_is_active_like "$state"; then
            [[ "$main_pid" =~ ^[0-9]+$ && "$main_pid" -gt 1 &&
               "$start_time" =~ ^[0-9]+$ && "$start_time" -gt 0 ]] \
                || die "active quiesce coordinator identity is invalid: $unit"
            if current_start=$(coordinator_process_start_time "$main_pid" 2>/dev/null); then
                [[ "$current_start" != "$start_time" ]] \
                    || die "captured coordinator process still exists outside its stopped unit: $unit"
            fi
        else
            [[ "$main_pid" == 0 && "$start_time" == 0 ]] \
                || die "inactive quiesce coordinator identity is not canonical: $unit"
        fi
        quiesce_active_states["$unit"]=$state
        quiesce_main_pids["$unit"]=$main_pid
        quiesce_start_times["$unit"]=$start_time
        count=$((count + 1))
    done < "$ledger"
    [[ "$count" -eq 2 ]] || die "quiesce coordinator ledger must contain exactly two rows"
}

verify_saved_enablement() {
    local unit actual
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        actual=$(systemctl is-enabled "$unit" 2>/dev/null || true)
        [[ "${actual:-unknown}" == "${saved_enabled_states[$unit]}" ]] \
            || die "installed enablement mismatch for $unit"
    done
}

verify_saved_runtime_states() {
    local unit state
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        state=${saved_active_states[$unit]}
        if service_state_is_active_like "$state"; then
            systemctl is-active --quiet "$unit" \
                || die "saved active-like service is not active: $unit"
        elif systemctl is-active --quiet "$unit"; then
            die "saved inactive-like service became active: $unit"
        fi
    done
}

validate_retention_snapshot() {
    local snapshot=$1 relative version
    [[ "$snapshot" == "$SNAPSHOT_ROOT/"* ]] \
        || die "unsafe snapshot path refused: $snapshot"
    relative=${snapshot#"$SNAPSHOT_ROOT/"}
    [[ -n "$relative" && "$relative" != */* ]] \
        || die "nested snapshot path refused: $snapshot"
    validate_root_trusted_dir_chain "$snapshot"
    if find "$snapshot" -type l -print -quit | grep -q .; then
        die "snapshot contains a symbolic link"
    fi
    if find "$snapshot" ! -type d ! -type f -print -quit | grep -q .; then
        die "snapshot contains a special filesystem object"
    fi
    [[ -f "$snapshot/snapshot.version" && ! -L "$snapshot/snapshot.version" ]] \
        || die "snapshot version is missing or unsafe"
    version=$(tr -d '[:space:]' < "$snapshot/snapshot.version")
    [[ "$version" == "$SNAPSHOT_VERSION" ]] \
        || die "unsupported snapshot version: $version"
    [[ -f "$snapshot/SHA256SUMS" && ! -L "$snapshot/SHA256SUMS" ]] \
        || die "snapshot checksum manifest is missing or unsafe"
    (
        cd "$snapshot"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "snapshot checksum verification failed"
}

validate_pending_rollback_snapshot() {
    local snapshot_name=$1 created target nonce entry owner group mode permissions
    local ledger identity_ledger transition links size
    IFS=$'\t' read -r created target nonce \
        < <(release_txn_parse_update_snapshot_name "$snapshot_name") \
        || die "pending rollback snapshot name is not canonical"
    [[ "$target" == "$PENDING_TARGET" ]] \
        || die "pending rollback snapshot target differs from --pending-target"
    pending_snapshot_path=$SNAPSHOT_ROOT/$snapshot_name
    [[ -d "$pending_snapshot_path" && ! -L "$pending_snapshot_path" ]] \
        || die "pending rollback final snapshot is missing or unsafe"
    validate_retention_snapshot "$pending_snapshot_path"
    for entry in celikpanel.db bin/panel bin/agent web/index.html \
        units/celikpanel-agent.service units/celikpanel-panel.service \
        firewall-unit.state agent-ledger.state agent-state-root \
        agent-state/service-mutations.json release-updater.state; do
        [[ -f "$pending_snapshot_path/$entry" &&
           ! -L "$pending_snapshot_path/$entry" ]] \
            || die "normal rollback snapshot payload is missing or unsafe: $entry"
    done
    [[ -d "$pending_snapshot_path/agent-state" &&
       ! -L "$pending_snapshot_path/agent-state" ]] \
        || die "normal rollback snapshot agent-state directory is missing or unsafe"
    [[ ! -e "$pending_snapshot_path/celikpanel.db-wal" &&
       ! -L "$pending_snapshot_path/celikpanel.db-wal" &&
       ! -e "$pending_snapshot_path/celikpanel.db-shm" &&
       ! -L "$pending_snapshot_path/celikpanel.db-shm" &&
       ! -e "$pending_snapshot_path/celikpanel.db-journal" &&
       ! -L "$pending_snapshot_path/celikpanel.db-journal" ]] \
        || die "normal rollback database snapshot must be standalone"
    panel_tls_snapshot_validate "$pending_snapshot_path/panel-tls" \
        || die "pending snapshot panel TLS compatibility payload is invalid"
    while IFS= read -r -d '' entry; do
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$entry") \
            || die "cannot inspect pending snapshot object"
        permissions=$((8#$mode))
        [[ "$owner" == 0 ]] &&
            (( (permissions & 0022) == 0 )) \
            || die "pending snapshot objects must be root-owned and group/other non-writable"
    done < <(find "$pending_snapshot_path" -mindepth 1 -print0)
    for entry in commit target-release.commit target-release.tree created-at-utc \
        service-states.tsv quiesce-coordinators.tsv snapshot-transition.state; do
        [[ -f "$pending_snapshot_path/$entry" &&
           ! -L "$pending_snapshot_path/$entry" ]] \
            || die "pending snapshot provenance file is missing or unsafe: $entry"
    done
    printf 'unknown\n' | cmp -s - "$pending_snapshot_path/commit" \
        || die "pending snapshot source provenance is not exact"
    printf '%s\n' "$PENDING_TARGET" |
        cmp -s - "$pending_snapshot_path/target-release.commit" \
        || die "pending snapshot target commit does not match"
    printf '%s\n' "$TRANSACTION_RELEASE_TREE" |
        cmp -s - "$pending_snapshot_path/target-release.tree" \
        || die "pending snapshot target tree does not match"
    printf '%s\n' "$created" |
        cmp -s - "$pending_snapshot_path/created-at-utc" \
        || die "pending snapshot timestamp does not match its name"
    transition=$pending_snapshot_path/snapshot-transition.state
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$transition") \
        || die "cannot inspect pending snapshot transition"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 &&
       "$links" == 1 && "$size" -gt 0 && "$size" -le 32 ]] \
        || die "pending snapshot transition metadata is invalid"
    printf 'normal\n' | cmp -s - "$transition" \
        || die "pending-rollback finalizer accepts only an exact normal v6 snapshot"
    printf '%s\n' "$AGENT_STATE_DIR" |
        cmp -s - "$pending_snapshot_path/agent-state-root" \
        || die "normal rollback snapshot agent-state root is incompatible"
    printf 'present\n' | cmp -s - "$pending_snapshot_path/agent-ledger.state" \
        || die "normal rollback snapshot must contain the durable agent ledger"
    [[ ! -e "$pending_snapshot_path/pre-ledger-transition.tsv" &&
       ! -L "$pending_snapshot_path/pre-ledger-transition.tsv" &&
       ! -e "$pending_snapshot_path/pre-ledger-transition.sha256" &&
       ! -L "$pending_snapshot_path/pre-ledger-transition.sha256" &&
       ! -e "$pending_snapshot_path/schema17-transition.tsv" &&
       ! -L "$pending_snapshot_path/schema17-transition.tsv" &&
       ! -e "$pending_snapshot_path/schema17-transition.sha256" &&
       ! -L "$pending_snapshot_path/schema17-transition.sha256" &&
       ! -e "$pending_snapshot_path/transition-preflight" &&
       ! -L "$pending_snapshot_path/transition-preflight" ]] \
        || die "normal rollback snapshot contains bootstrap transition payloads"
    release_updater_state=$(tr -d '[:space:]' < "$pending_snapshot_path/release-updater.state")
    case "$release_updater_state" in
        present)
            [[ -f "$pending_snapshot_path/libexec/get.sh" &&
               ! -L "$pending_snapshot_path/libexec/get.sh" ]] \
                || die "snapshot release updater is marked present but missing or unsafe"
            [[ "$(stat -Lc '%u:%g:%a:%h' -- "$pending_snapshot_path/libexec/get.sh")" == 0:0:755:1 ]] \
                || die "snapshot release updater metadata is unsafe"
            ;;
        absent)
            [[ ! -e "$pending_snapshot_path/libexec/get.sh" &&
               ! -L "$pending_snapshot_path/libexec/get.sh" ]] \
                || die "snapshot release updater is marked absent but includes bytes"
            ;;
        *) die "normal rollback snapshot release-updater state is invalid" ;;
    esac
    firewall_state=$(tr -d '[:space:]' < "$pending_snapshot_path/firewall-unit.state")
    case "$firewall_state" in
        present)
            [[ -f "$pending_snapshot_path/units/celikpanel-firewall-restore.service" &&
               ! -L "$pending_snapshot_path/units/celikpanel-firewall-restore.service" ]] \
                || die "snapshot firewall unit is marked present but missing or unsafe"
            ;;
        absent)
            [[ ! -e "$pending_snapshot_path/units/celikpanel-firewall-restore.service" &&
               ! -L "$pending_snapshot_path/units/celikpanel-firewall-restore.service" ]] \
                || die "snapshot firewall unit is marked absent but includes bytes"
            ;;
        *) die "normal rollback snapshot firewall-unit state is invalid" ;;
    esac
    ledger=$pending_snapshot_path/service-states.tsv
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$ledger") \
        || die "cannot inspect pending snapshot service-state ledger"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 &&
       "$links" == 1 && "$size" -gt 0 && "$size" -le 4096 ]] \
        || die "pending snapshot service-state ledger metadata is invalid"
    identity_ledger=$pending_snapshot_path/quiesce-coordinators.tsv
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$identity_ledger") \
        || die "cannot inspect pending snapshot coordinator ledger"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 &&
       "$links" == 1 && "$size" -gt 0 && "$size" -le 1024 ]] \
        || die "pending snapshot coordinator ledger metadata is invalid"
    _release_txn_validate_quiesce_coordinators "$identity_ledger" \
        || die "pending snapshot coordinator ledger failed canonical validation"
    load_saved_service_states "$ledger"
    case "$firewall_state:${saved_enabled_states[celikpanel-firewall-restore.service]}" in
        absent:not-found|present:*) ;;
        *) die "snapshot firewall-unit state disagrees with its service ledger" ;;
    esac
    panel_tls_snapshot_scheduler_matches_service_ledger \
        "$pending_snapshot_path/panel-tls" "$ledger" \
        || die "pending snapshot TLS scheduler state disagrees with the service ledger"
    load_quiesce_coordinator_identities "$identity_ledger"
}

verify_installed_rollback_artifacts() {
    local unit
    validate_binary "$BIN_DIR/panel" installed-panel
    validate_binary "$BIN_DIR/agent" installed-agent
    [[ -x "$pending_snapshot_path/bin/panel" &&
       -x "$pending_snapshot_path/bin/agent" ]] \
        || die "normal rollback snapshot binaries are not executable"
    cmp -s "$pending_snapshot_path/bin/panel" "$BIN_DIR/panel" \
        || die "installed panel bytes differ from the rollback snapshot"
    cmp -s "$pending_snapshot_path/bin/agent" "$BIN_DIR/agent" \
        || die "installed agent bytes differ from the rollback snapshot"
    case "$release_updater_state" in
        present)
            [[ -f "$RELEASE_UPDATER" && ! -L "$RELEASE_UPDATER" ]] \
                || die "restored release updater is missing or unsafe"
            [[ "$(stat -Lc '%u:%g:%a:%h' -- "$RELEASE_UPDATER")" == 0:0:755:1 ]] \
                || die "restored release updater metadata differs from snapshot policy"
            cmp -s "$pending_snapshot_path/libexec/get.sh" "$RELEASE_UPDATER" \
                || die "restored release updater bytes differ from the rollback snapshot"
            ;;
        absent)
            [[ ! -e "$RELEASE_UPDATER" && ! -L "$RELEASE_UPDATER" ]] \
                || die "release updater exists although the rollback snapshot marks it absent"
            ;;
        *) die "validated release-updater state was lost" ;;
    esac
    validate_root_trusted_dir_chain "$WEB_DIR"
    if find "$BIN_DIR" "$WEB_DIR" -type l -print -quit | grep -q .; then
        die "restored binary/web tree contains a symbolic link"
    fi
    if find "$BIN_DIR" "$WEB_DIR" ! -type d ! -type f -print -quit | grep -q .; then
        die "restored binary/web tree contains a special filesystem object"
    fi
    cmp -s \
        <(cd "$pending_snapshot_path/web" &&
            LC_ALL=C find . -mindepth 1 -printf '%y\t%p\n' | LC_ALL=C sort) \
        <(cd "$WEB_DIR" &&
            LC_ALL=C find . -mindepth 1 -printf '%y\t%p\n' | LC_ALL=C sort) \
        || die "restored web tree structure differs from the rollback snapshot"
    cmp -s \
        <(cd "$pending_snapshot_path/web" &&
            LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
        <(cd "$WEB_DIR" &&
            LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
        || die "restored web tree differs from the rollback snapshot"
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        [[ -f "$UNIT_DIR/$unit" && ! -L "$UNIT_DIR/$unit" ]] \
            || die "restored unit is missing or unsafe: $unit"
        cmp -s "$pending_snapshot_path/units/$unit" "$UNIT_DIR/$unit" \
            || die "restored unit differs from the rollback snapshot: $unit"
    done
    if [[ "$firewall_state" == present ]]; then
        [[ -f "$UNIT_DIR/celikpanel-firewall-restore.service" &&
           ! -L "$UNIT_DIR/celikpanel-firewall-restore.service" ]] \
            || die "restored firewall unit is missing or unsafe"
        cmp -s "$pending_snapshot_path/units/celikpanel-firewall-restore.service" \
            "$UNIT_DIR/celikpanel-firewall-restore.service" \
            || die "restored firewall unit differs from the rollback snapshot"
    elif [[ -e "$UNIT_DIR/celikpanel-firewall-restore.service" ||
            -L "$UNIT_DIR/celikpanel-firewall-restore.service" ]]; then
        die "firewall unit exists although the rollback snapshot marks it absent"
    fi
    [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || die "restored durable agent ledger is missing or unsafe"
    cmp -s "$pending_snapshot_path/agent-state/service-mutations.json" "$AGENT_LEDGER" \
        || die "restored durable agent ledger differs from the rollback snapshot"
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
        [[ "$loaded" != "$target" ]] || found=1
    done
    [[ "$found" == 1 ]] \
        || die "systemd did not load the exact runtime-directory preserve drop-in"
    manager_value=$("$SYSTEMCTL_BIN" show --property=RuntimeDirectoryPreserve --value celikpanel-agent.service) \
        || die "cannot inspect loaded RuntimeDirectoryPreserve value"
    [[ "$manager_value" == yes ]] \
        || die "systemd did not load RuntimeDirectoryPreserve=yes for the agent"
}

prepare_runtime_mutation_lock_dir() {
    local lock_dir parent expected_group owner group mode links size entry
    local dotglob_was_set=0 nullglob_was_set=0
    local -a entries=()
    lock_dir=$(dirname -- "$MUTATION_LOCK")
    [[ "$lock_dir" == /run/celikpanel ]] \
        || die "unexpected mutation lock directory"
    parent=$(dirname -- "$lock_dir")
    [[ "$parent" == /run && -d "$parent" && ! -L "$parent" ]] \
        || die "unsafe mutation lock parent"
    validate_root_trusted_dir_chain "$parent"
    expected_group=$(getent group celikpanel | cut -d: -f3) \
        || die "celikpanel group is unavailable"
    [[ "$expected_group" =~ ^[0-9]+$ && "$expected_group" -gt 0 ]] \
        || die "celikpanel group id is invalid"
    if [[ -e "$lock_dir" || -L "$lock_dir" ]]; then
        [[ -d "$lock_dir" && ! -L "$lock_dir" ]] \
            || die "mutation lock directory exists but is unsafe"
    else
        install -d -m 0750 -o root -g celikpanel -- "$lock_dir" \
            || die "cannot prepare mutation lock directory"
    fi
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
    shopt -q dotglob && dotglob_was_set=1
    shopt -q nullglob && nullglob_was_set=1
    shopt -s dotglob nullglob
    entries=("$lock_dir"/*)
    (( dotglob_was_set == 1 )) || shopt -u dotglob
    (( nullglob_was_set == 1 )) || shopt -u nullglob
    (( ${#entries[@]} <= 2 )) \
        || die "mutation lock directory contains unexpected entries"
    for entry in "${entries[@]}"; do
        case "$entry" in
            "$MUTATION_LOCK")
                [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] \
                    || die "existing mutation lock is unsafe"
                read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$MUTATION_LOCK") \
                    || die "cannot inspect existing mutation lock"
                [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 600 &&
                   "$links" == 1 && "$size" == 0 ]] \
                    || die "existing mutation lock metadata is unsafe"
                ;;
            "$AGENT_SOCKET")
                [[ -S "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]] \
                    || die "stale agent socket path is not an exact non-symlink socket"
                ;;
            *) die "mutation lock directory contains an unexpected entry: $entry" ;;
        esac
    done
}

remove_stale_agent_socket_under_lock() {
    local socket_identity
    verify_mutation_lock_held
    verify_both_units_stopped
    if [[ -e "$AGENT_SOCKET" || -L "$AGENT_SOCKET" ]]; then
        [[ -S "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]] \
            || die "agent socket path is unsafe under the mutation lock"
        socket_identity=$(stat -Lc '%d:%i' -- "$AGENT_SOCKET") \
            || die "cannot identify stale agent socket"
        rm -- "$AGENT_SOCKET" \
            || die "cannot remove stale agent socket under the mutation lock"
        [[ ! -e "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]] \
            || die "stale agent socket still exists after locked cleanup"
        printf '%s\n' "==> Removed stale stopped-agent socket inode: $socket_identity"
    fi
}

open_mutation_lock() {
    local acquire_mode=$1 expected_group owner group mode links size path_identity fd_identity
    case "$acquire_mode" in
        immediate) ;;
        handoff) ;;
        *) die "unknown mutation lock acquisition mode: $acquire_mode" ;;
    esac
    expected_group=$(getent group celikpanel | cut -d: -f3) \
        || die "celikpanel group is unavailable"
    if [[ ! -e "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]]; then
        [[ "$acquire_mode" == immediate ]] \
            || die "mutation lock pathname disappeared before controlled handoff reacquire"
        (umask 077; set -o noclobber; : > "$MUTATION_LOCK") \
            || die "cannot exclusively create mutation lock"
        chown root:celikpanel -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock ownership"
        chmod 0600 -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock permissions"
    fi
    [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] \
        || die "mutation lock pathname is unsafe"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$MUTATION_LOCK") \
        || die "cannot inspect mutation lock"
    [[ "$owner" == 0 && "$group" == "$expected_group" && "$mode" == 600 &&
       "$links" == 1 && "$size" == 0 ]] \
        || die "mutation lock must be empty root:celikpanel mode 0600 with one link"
    path_identity=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") \
        || die "cannot identify mutation lock"
    if [[ "$acquire_mode" == handoff ]]; then
        [[ "$path_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
            || die "mutation lock inode changed across the controlled agent start"
    else
        MUTATION_LOCK_IDENTITY=$path_identity
    fi
    exec {MUTATION_LOCK_FD}<>"$MUTATION_LOCK"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify opened mutation lock"
    [[ "$fd_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
        || die "opened mutation lock does not match the recorded inode"
    if [[ "$acquire_mode" == handoff ]]; then
        flock -n -x "$MUTATION_LOCK_FD" \
            || die "another mutation entered during the controlled agent-start handoff"
    else
        flock -n -x "$MUTATION_LOCK_FD" \
            || die "a service or package mutation is active"
    fi
}

verify_mutation_lock_held() {
    local path_identity fd_identity
    [[ "$MUTATION_LOCK_FD" =~ ^[0-9]+$ &&
       -e "/proc/$BASHPID/fd/$MUTATION_LOCK_FD" ]] \
        || die "mutation lock descriptor is not open"
    path_identity=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") \
        || die "cannot identify mutation lock pathname"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify mutation lock descriptor"
    [[ "$path_identity" == "$MUTATION_LOCK_IDENTITY" &&
       "$fd_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
        || die "mutation lock pathname or descriptor identity changed"
    flock -n -x "$MUTATION_LOCK_FD" \
        || die "mutation flock ownership changed"
}

release_mutation_lock() {
    [[ -n "${MUTATION_LOCK_FD:-}" ]] || return 0
    flock -u "$MUTATION_LOCK_FD" || return 1
    exec {MUTATION_LOCK_FD}>&-
    MUTATION_LOCK_FD=
}

run_target_agent_idle_unlocked() {
    env -i PATH="$PATH" HOME=/root LC_ALL=C \
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \
        CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$TRUSTED_TRANSACTION_RELEASE/bin/agent" --check-service-mutation-idle \
        || die "rollback transaction checker found agent/package mutations active"
}

run_target_agent_idle_locked() {
    verify_mutation_lock_held
    env -i PATH="$PATH" HOME=/root LC_ALL=C \
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \
        CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$TRUSTED_TRANSACTION_RELEASE/bin/agent" \
        --check-service-mutation-idle-under-external-lock \
        || die "restored agent mutation ledger is not idle under the external lock"
    verify_mutation_lock_held
}

run_target_panel_wal_idle() {
    env -i PATH="$PATH" HOME=/root LC_ALL=C \
        CELIKPANEL_DATA_DIR="$PANEL_DATA_DIR" \
        "$TRUSTED_TRANSACTION_RELEASE/bin/panel" \
        --check-service-operations-idle-wal-aware \
        || die "restored panel operation ledger is not WAL-aware idle"
}

verify_pending_evidence() {
    release_txn_validate_pending_token \
        "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
        || die "completion.pending marker changed during finalization"
    if [[ -e "$TRANSACTION_ROOT/scheduler-restore.pending" ||
          -L "$TRANSACTION_ROOT/scheduler-restore.pending" ]]; then
        release_txn_validate_scheduler_restore_token \
            "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
            || die "scheduler-restore.pending does not match completion.pending"
    fi
    validate_pending_rollback_snapshot "$pending_snapshot"
    verify_installed_rollback_artifacts
    verify_saved_enablement
}

wait_for_agent_ready() {
    local _
    [[ ! -e "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]] \
        || die "agent socket exists before the controlled agent start"
    systemctl start celikpanel-agent.service \
        || die "restored rollback agent could not be started"
    for _ in $(seq 1 50); do
        if systemctl is-active --quiet celikpanel-agent.service &&
           [[ -S "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]]; then
            return 0
        fi
        sleep 0.2
    done
    die "restored rollback agent did not become active with its exact socket"
}

wait_for_panel_ready() {
    local _
    systemctl start celikpanel-panel.service \
        || die "restored rollback panel could not be started"
    for _ in $(seq 1 50); do
        systemctl is-active --quiet celikpanel-panel.service && return 0
        sleep 0.2
    done
    die "restored rollback panel did not become active"
}

# EXIT cleanup must not claim a fail-closed stop from systemctl's request
# result alone. Prove ActiveState, MainPID and every recursive cgroup.procs;
# if graceful stop is not enough, terminate the already transaction-owned
# coordinator cgroups and repeat the same bounded proof.
coordinators_stopped_trap_safe() {
    local unit state job main_pid pid_output
    for unit in celikpanel-panel.service celikpanel-agent.service; do
        job=$(systemctl show --property=Job --value "$unit" 2>/dev/null) || return 1
        [[ -z "$job" ]] || return 1
        state=$(systemctl show --property=ActiveState --value "$unit" 2>/dev/null) || return 1
        case "$state" in inactive|failed) ;; *) return 1 ;; esac
        main_pid=$(systemctl show --property=MainPID --value "$unit" 2>/dev/null) || return 1
        [[ "$main_pid" == 0 ]] || return 1
        pid_output=$(service_cgroup_pids "$unit" 2>/dev/null) || return 1
        [[ -z "$pid_output" ]] || return 1
    done
}

stop_coordinators_trap_safe() {
    local unit _
    systemctl stop --no-block celikpanel-panel.service >/dev/null 2>&1 || true
    systemctl stop --no-block celikpanel-agent.service >/dev/null 2>&1 || true
    for _ in $(seq 1 30); do
        coordinators_stopped_trap_safe && return 0
        sleep 0.05
    done
    for unit in celikpanel-panel.service celikpanel-agent.service; do
        systemctl kill --kill-whom=all --signal=SIGKILL "$unit" >/dev/null 2>&1 || true
        systemctl stop --no-block "$unit" >/dev/null 2>&1 || true
    done
    for _ in $(seq 1 50); do
        coordinators_stopped_trap_safe && return 0
        sleep 0.05
    done
    return 1
}

finalization_exit() {
    local status=$? stop_proved=0 authorization_closed=1 retry_marker_proved=0
    trap - EXIT
    if [[ "$status" -ne 0 && "$finalization_succeeded" -eq 0 ]]; then
        if [[ "$scheduler_restore_completed" -eq 1 &&
              ! -e "$TRANSACTION_ROOT/scheduler-restore.pending" &&
              ! -L "$TRANSACTION_ROOT/scheduler-restore.pending" ]]; then
            # The scheduler restore itself succeeded. Marker removal became
            # visible but its parent-directory durability proof failed, so do
            # not turn a completed runtime into a markerless outage. This is
            # deliberately still a failed finalization, not assumed success.
            if ! release_mutation_lock >/dev/null 2>&1; then
                printf '%s\n' \
                    "!! Certbot scheduler restoration completed, but the mutation lock did not release cleanly." >&2
            fi
            printf '%s\n' \
                "!! Certbot scheduler restoration completed; durable marker removal is uncertain. Runtime was left intact and finalization did not claim success." >&2
            return "$status"
        fi
        if [[ ( "$completion_verified" -eq 1 &&
                "$completion_removing" -eq 1 &&
                ! -e "$TRANSACTION_ROOT/completion.pending" &&
                ! -L "$TRANSACTION_ROOT/completion.pending" ) ||
              ( "$scheduler_only_resume" -eq 1 &&
                "$scheduler_recovery_verified" -eq 1 ) ]]; then
            # Runtime completion is safe to preserve only when the exact durable
            # scheduler obligation remains. A missing or tampered marker falls
            # through to the fail-closed path below.
            if [[ -e "$TRANSACTION_ROOT/scheduler-restore.pending" ||
                  -L "$TRANSACTION_ROOT/scheduler-restore.pending" ]] &&
               release_txn_validate_scheduler_restore_token \
                   "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot"; then
                if ! release_mutation_lock >/dev/null 2>&1; then
                    printf '%s\n' \
                        "!! Exact scheduler recovery marker remains, but the mutation lock did not release cleanly." >&2
                fi
                printf '%s\n' \
                    "!! Runtime completion is durable; exact Certbot scheduler restoration remains safely retryable." >&2
                return "$status"
            fi
        fi
        if [[ "$start_authorization_created" -eq 1 ]]; then
            if release_txn_remove_start_authorization \
                "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
                "$RELEASE_TRANSACTION_FD" "$pending_token" rollback "$pending_snapshot" \
                >/dev/null 2>&1; then
                start_authorization_created=0
            else
                authorization_closed=0
            fi
        fi
        if stop_coordinators_trap_safe && [[ "$authorization_closed" -eq 1 ]]; then
            stop_proved=1
        fi
        if [[ ( -e "$TRANSACTION_ROOT/completion.pending" ||
                -L "$TRANSACTION_ROOT/completion.pending" ) ]] &&
           release_txn_validate_pending_token \
               "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
               >/dev/null 2>&1; then
            retry_marker_proved=1
        elif [[ ( -e "$TRANSACTION_ROOT/scheduler-restore.pending" ||
                  -L "$TRANSACTION_ROOT/scheduler-restore.pending" ) ]] &&
             release_txn_validate_scheduler_restore_token \
                 "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
                 >/dev/null 2>&1; then
            retry_marker_proved=1
        fi
        release_mutation_lock >/dev/null 2>&1 || true
        if [[ "$stop_proved" -eq 0 ]]; then
            if [[ "$retry_marker_proved" -eq 1 ]]; then
                printf '%s\n' \
                    "!! Finalization failed; coordinator stopped state could not be proved. The exact durable transaction marker remains for operator recovery." >&2
            else
                printf '%s\n' \
                    "!! Finalization failed; coordinator stopped state could not be proved and durable marker state requires operator verification." >&2
            fi
        elif [[ -e "$TRANSACTION_ROOT/completion.pending" ||
                -L "$TRANSACTION_ROOT/completion.pending" ]]; then
            printf '%s\n' \
                "!! Finalization failed closed; completion.pending was preserved and both coordinators were proved stopped." >&2
        else
            printf '%s\n' \
                "!! Finalization failed closed and both coordinators were proved stopped, but completion.pending is unexpectedly absent." >&2
        fi
    fi
    return "$status"
}

acquire_release_transaction_lock
validate_immutable_release \
    recovery "$TRUSTED_RECOVERY_RELEASE" "$RECOVERY_COMMIT" \
    deploy/finalize-pending-rollback.sh
validate_immutable_release \
    transaction "$TRUSTED_TRANSACTION_RELEASE" "$PENDING_TARGET" ''

# The verified recovery release, never the historical transaction release, owns the current
# marker parser and the persistent systemd start guard installed below.
TRUSTED_RELEASE_ROOT=$TRUSTED_RECOVERY_RELEASE
# shellcheck source=deploy/release-transaction-guard.sh
source "$TRUSTED_RECOVERY_RELEASE/deploy/release-transaction-guard.sh"
# shellcheck source=deploy/panel-tls-snapshot.sh
source "$TRUSTED_RECOVERY_RELEASE/deploy/panel-tls-snapshot.sh"
release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "recovery guard rejected the inherited release transaction lock"

marker_count=0
quiesce_present=0
active_present=0
completion_present=0
scheduler_present=0
[[ -e "$TRANSACTION_ROOT/quiesce.pending" ||
   -L "$TRANSACTION_ROOT/quiesce.pending" ]] && quiesce_present=1
[[ -e "$TRANSACTION_ROOT/active" ||
   -L "$TRANSACTION_ROOT/active" ]] && active_present=1
[[ -e "$TRANSACTION_ROOT/completion.pending" ||
   -L "$TRANSACTION_ROOT/completion.pending" ]] && completion_present=1
[[ -e "$TRANSACTION_ROOT/scheduler-restore.pending" ||
   -L "$TRANSACTION_ROOT/scheduler-restore.pending" ]] && scheduler_present=1
for marker_name in quiesce.pending active completion.pending scheduler-restore.pending; do
    [[ -e "$TRANSACTION_ROOT/$marker_name" ||
       -L "$TRANSACTION_ROOT/$marker_name" ]] &&
        marker_count=$((marker_count + 1))
done
[[ "$quiesce_present" -eq 0 && "$active_present" -eq 0 ]] \
    || die "finalizer refuses quiesce.pending or active transaction topology"
if [[ "$completion_present" -eq 1 ]]; then
    [[ ( "$marker_count" -eq 1 || "$marker_count" -eq 2 ) &&
       -f "$TRANSACTION_ROOT/completion.pending" &&
       ! -L "$TRANSACTION_ROOT/completion.pending" ]] \
        || die "finalizer requires completion.pending with at most its matching scheduler marker"
    IFS=$'\t' read -r pending_token pending_operation pending_snapshot \
        < <(release_txn_read_pending_fields "$TRANSACTION_ROOT") \
        || die "cannot read the exact completion.pending marker"
    [[ "$pending_operation" == rollback ]] \
        || die "pending-rollback finalizer refuses a non-rollback completion marker"
    release_txn_validate_pending_token \
        "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
        || die "completion.pending marker failed exact validation"
    if [[ "$scheduler_present" -eq 1 ]]; then
        release_txn_validate_scheduler_restore_token \
            "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
            || die "scheduler-restore.pending does not exactly match completion.pending"
        scheduler_restore_pending=1
    fi
elif [[ "$marker_count" -eq 1 && "$scheduler_present" -eq 1 ]]; then
    IFS=$'\t' read -r pending_token pending_operation pending_snapshot \
        < <(release_txn_read_scheduler_restore_fields "$TRANSACTION_ROOT") \
        || die "cannot read the exact scheduler-restore.pending marker"
    [[ "$pending_operation" == rollback ]] \
        || die "pending-rollback finalizer refuses a non-rollback scheduler marker"
    scheduler_only_resume=1
    scheduler_restore_pending=1
    trap finalization_exit EXIT
    release_txn_validate_scheduler_restore_token \
        "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
        || die "scheduler-only marker failed exact validation"
else
    die "finalizer requires completion.pending or an exact scheduler-only recovery marker"
fi

validate_pending_rollback_snapshot "$pending_snapshot"
verify_installed_rollback_artifacts
verify_saved_enablement
if [[ "$scheduler_only_resume" -eq 1 ]]; then
    verify_saved_runtime_states
    scheduler_recovery_verified=1
    release_txn_validate_scheduler_restore_token \
        "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
        || die "scheduler-only marker changed after snapshot verification"
    panel_tls_quiesce_certbot_scheduler "$pending_snapshot_path/panel-tls" \
        || die "Certbot renewal scheduler could not be re-quiesced for exact recovery"
    release_txn_validate_scheduler_restore_token \
        "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
        || die "scheduler-only marker changed during exact recovery"
    panel_tls_restore_certbot_scheduler "$pending_snapshot_path/panel-tls" \
        || die "Certbot renewal scheduler state could not be restored"
    scheduler_restore_completed=1
    release_txn_remove_scheduler_restore_pending \
        "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$pending_token" rollback "$pending_snapshot" \
        || die "cannot remove the exact scheduler-only recovery marker"
    scheduler_restore_pending=0
    finalization_succeeded=1
    trap - EXIT
    printf '\n%s\n' \
        "==> Pending rollback runtime was already complete; Certbot scheduler restoration is complete."
    exit 0
fi
if [[ "$completion_present" -eq 1 && "$scheduler_present" -eq 1 ]]; then
    # A crash after the durable scheduler marker was written but before the
    # completion marker was removed can leave restored coordinators active
    # without this process owning the old mutation-lock descriptor. Never use
    # that topology as a shortcut. The trap stops both coordinators while both
    # exact markers remain; the next exact invocation uses the complete
    # lock/idle/WAL-aware finalization path below.
    trap finalization_exit EXIT
fi
verify_both_units_stopped
trap finalization_exit EXIT

install_and_verify_runtime_directory_preserve
release_txn_install_and_verify_unit_guards \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$TRANSACTION_START_HELPER" "$RELEASE_TRANSACTION_FD" "$SYSTEMCTL_BIN" \
    "" "$UNIT_DIR/celikpanel-agent.service.d/09-runtime-directory-preserve.conf" \
    || die "recovery release transaction guards could not be installed"
release_txn_clear_stale_start_authorization \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "stale start authorization could not be cleared"
verify_both_units_stopped
verify_pending_evidence

prepare_runtime_mutation_lock_dir
install_and_verify_runtime_directory_preserve
release_txn_install_and_verify_unit_guards \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$TRANSACTION_START_HELPER" "$RELEASE_TRANSACTION_FD" "$SYSTEMCTL_BIN" \
    "" "$UNIT_DIR/celikpanel-agent.service.d/09-runtime-directory-preserve.conf" \
    || die "transaction guards changed during runtime-lock preparation"
release_txn_clear_stale_start_authorization \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "stale authorization reappeared during runtime-lock preparation"
verify_both_units_stopped
verify_pending_evidence

run_target_agent_idle_unlocked
open_mutation_lock immediate
verify_mutation_lock_held
remove_stale_agent_socket_under_lock
run_target_agent_idle_locked
run_target_panel_wal_idle
verify_both_units_stopped
panel_tls_quiesce_certbot_scheduler "$pending_snapshot_path/panel-tls" \
    || die "Certbot renewal scheduler could not be quiesced for pending-rollback recovery"
verify_mutation_lock_held
verify_both_units_stopped
panel_tls_secure_restore_parent "$PANEL_TLS_DIR" \
    || die "panel TLS data parent could not be secured for pending rollback"
panel_tls_restore_snapshot \
    "$pending_snapshot_path/panel-tls" "$PANEL_TLS_DIR" \
    "$PANEL_CERT_PENDING" "$PANEL_CERT_HOOK" \
    || die "panel TLS compatibility state could not be restored for pending rollback"
panel_tls_restore_service_parent "$PANEL_TLS_DIR" \
    || die "panel TLS data parent service ownership could not be restored for pending rollback"
verify_pending_evidence
verify_both_units_stopped
sync -f -- "$PANEL_DB" "$PANEL_DATA_DIR" \
    || die "restored rollback database could not be made durable"
verify_pending_evidence
verify_both_units_stopped

release_txn_create_start_authorization \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
    "$RELEASE_TRANSACTION_FD" "$pending_token" rollback "$pending_snapshot" \
    || die "cannot authorize the exact restored rollback controlled starts"
start_authorization_created=1

if service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
    # Agent startup reconciles the durable mutation ledger before publishing
    # its RPC socket. Release the flock only for this required startup, then
    # reject any intervening mutation by reacquiring the same inode without
    # waiting.
    release_mutation_lock \
        || die "cannot release mutation lock for the controlled agent-start handoff"
    wait_for_agent_ready
    open_mutation_lock handoff
else
    # No startup reconciliation is needed. Retain the flock continuously.
    [[ ! -e "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]] \
        || die "inactive saved agent unexpectedly has a socket"
fi

# The panel remains stopped. Both branches reach this common proof while
# holding the original mutation-lock inode.
verify_mutation_lock_held
run_target_agent_idle_locked
verify_pending_evidence

if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}"; then
    wait_for_panel_ready
fi
verify_saved_runtime_states
run_target_agent_idle_locked
verify_pending_evidence

# A controlled panel start may leave a healthy non-empty SQLite WAL. Stop it
# while start authorization and the original mutation-lock inode are still
# owned, prove the restored normal database WAL-aware idle, make it durable,
# then restore the exact saved active state.
if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}"; then
    systemctl stop celikpanel-panel.service \
        || die "restored rollback panel could not be stopped for the final WAL-aware proof"
    panel_state=$(systemctl show --property=ActiveState --value celikpanel-panel.service) \
        || die "cannot inspect rollback panel after its controlled stop"
    [[ "$panel_state" == inactive || "$panel_state" == failed ]] \
        || die "rollback panel did not stop for the final WAL-aware proof"
fi
run_target_panel_wal_idle
sync -f -- "$PANEL_DB" "$PANEL_DATA_DIR" \
    || die "restored rollback database final state could not be made durable"
if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}"; then
    wait_for_panel_ready
fi
verify_saved_runtime_states
run_target_agent_idle_locked
verify_pending_evidence

release_txn_remove_start_authorization \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
    "$RELEASE_TRANSACTION_FD" "$pending_token" rollback "$pending_snapshot" \
    || die "cannot remove exact restored rollback start authorization"
start_authorization_created=0

verify_saved_runtime_states
run_target_agent_idle_locked
verify_pending_evidence
completion_verified=1
completion_removing=1
release_txn_mark_scheduler_restore_pending \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$pending_token" rollback "$pending_snapshot" \
    || die "cannot durably record pending Certbot scheduler restoration"
scheduler_restore_pending=1
release_txn_remove_completion_pending \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$pending_token" rollback "$pending_snapshot" \
    || die "cannot remove the exact completion.pending marker"
release_mutation_lock \
    || die "cannot release terminal pending-rollback mutation lock"
panel_tls_quiesce_certbot_scheduler "$pending_snapshot_path/panel-tls" \
    || die "Certbot renewal scheduler could not be re-quiesced before restoration"
release_txn_validate_scheduler_restore_token \
    "$TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
    || die "scheduler-restore.pending changed before restoration"
panel_tls_restore_certbot_scheduler "$pending_snapshot_path/panel-tls" \
    || die "Certbot renewal scheduler state could not be restored"
scheduler_restore_completed=1
release_txn_remove_scheduler_restore_pending \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$pending_token" rollback "$pending_snapshot" \
    || die "cannot remove the exact scheduler-restore.pending marker"
scheduler_restore_pending=0
finalization_succeeded=1
trap - EXIT

printf '\n%s\n' "==> Pending rollback finalized from dual verified provenance."
printf '%s\n' "    Transaction target: $PENDING_TARGET"
printf '%s\n' "    Recovery commit: $RECOVERY_COMMIT"
printf '%s\n' "    Snapshot: $pending_snapshot_path"
printf '%s\n' "==> Run a fresh normal update from the current reviewed release; do not rerun the historical target updater."
