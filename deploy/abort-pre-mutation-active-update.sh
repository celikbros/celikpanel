#!/usr/bin/env bash
# Abort one exact active update only when it is still the legacy pre-ledger
# scaffold and every installed byte still equals the operator-reviewed previous
# release. This is an incident recovery tool, not a generic rollback path.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

RELEASES_ROOT=/var/backups/celikpanel/releases
SNAPSHOT_ROOT=/var/backups/celikpanel/update-snapshots
TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
TRANSACTION_START_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
UNIT_DIR=/etc/systemd/system
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
AGENT_SOCKET=/run/celikpanel/agent.sock
INSTALLED_ROOT=/opt/celikpanel
BIN_DIR=$INSTALLED_ROOT/bin
WEB_DIR=$INSTALLED_ROOT/web
PREVIOUS_RELEASES_ROOT=$INSTALLED_ROOT/releases

ACTIVE_TARGET=
ACTIVE_TOKEN=
ACTIVE_SNAPSHOT=
EXPECTED_STAGE=
PREVIOUS_RELEASE_COMMIT=
PREVIOUS_AGENT_SHA256=
PREVIOUS_PANEL_SHA256=
PREVIOUS_WEB_SHA256=
RECOVERY_COMMIT=
TRUSTED_RELEASE_ROOT=
PREVIOUS_RELEASE_ROOT=
RELEASE_TRANSACTION_FD=
MUTATION_LOCK_FD=
MUTATION_LOCK_IDENTITY=
active_marker_identity=
active_marker_digest=
verification_tmp=
marker_removal_begun=0
recovery_succeeded=0

declare -A saved_enabled_states=()
declare -A saved_active_states=()
declare -A quiesce_active_states=()
declare -A quiesce_main_pids=()
declare -A quiesce_start_times=()

die() {
    printf '!! %s\n' "$*" >&2
    exit 1
}

usage() {
    printf '%s\n' \
        "usage: $0 --active-target=<40-lowercase-hex> --active-token=<64-lowercase-hex> --active-snapshot=<canonical-snapshot-name> --expected-stage=<absolute-canonical-stage> --previous-release-commit=<40-lowercase-hex> --previous-agent-sha256=<64-lowercase-hex> --previous-panel-sha256=<64-lowercase-hex> --previous-web-sha256=<64-lowercase-hex> --recovery-commit=<40-lowercase-hex> --trusted-release=<absolute-immutable-release-root>" >&2
    exit 2
}

for argument in "$@"; do
    case "$argument" in
        --active-target=*)
            [[ -z "$ACTIVE_TARGET" ]] || usage
            ACTIVE_TARGET=${argument#--active-target=}
            ;;
        --active-token=*)
            [[ -z "$ACTIVE_TOKEN" ]] || usage
            ACTIVE_TOKEN=${argument#--active-token=}
            ;;
        --active-snapshot=*)
            [[ -z "$ACTIVE_SNAPSHOT" ]] || usage
            ACTIVE_SNAPSHOT=${argument#--active-snapshot=}
            ;;
        --expected-stage=*)
            [[ -z "$EXPECTED_STAGE" ]] || usage
            EXPECTED_STAGE=${argument#--expected-stage=}
            ;;
        --previous-release-commit=*)
            [[ -z "$PREVIOUS_RELEASE_COMMIT" ]] || usage
            PREVIOUS_RELEASE_COMMIT=${argument#--previous-release-commit=}
            ;;
        --previous-agent-sha256=*)
            [[ -z "$PREVIOUS_AGENT_SHA256" ]] || usage
            PREVIOUS_AGENT_SHA256=${argument#--previous-agent-sha256=}
            ;;
        --previous-panel-sha256=*)
            [[ -z "$PREVIOUS_PANEL_SHA256" ]] || usage
            PREVIOUS_PANEL_SHA256=${argument#--previous-panel-sha256=}
            ;;
        --previous-web-sha256=*)
            [[ -z "$PREVIOUS_WEB_SHA256" ]] || usage
            PREVIOUS_WEB_SHA256=${argument#--previous-web-sha256=}
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

[[ $# -eq 10 &&
   "$ACTIVE_TARGET" =~ ^[0-9a-f]{40}$ &&
   "$ACTIVE_TOKEN" =~ ^[0-9a-f]{64}$ &&
   "$ACTIVE_SNAPSHOT" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ &&
   "$PREVIOUS_RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ &&
   "$PREVIOUS_AGENT_SHA256" =~ ^[0-9a-f]{64}$ &&
   "$PREVIOUS_PANEL_SHA256" =~ ^[0-9a-f]{64}$ &&
   "$PREVIOUS_WEB_SHA256" =~ ^[0-9a-f]{64}$ &&
   "$RECOVERY_COMMIT" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "$EXPECTED_STAGE" == "$SNAPSHOT_ROOT/"* &&
   "$TRUSTED_RELEASE_ROOT" == /* ]] || usage
[[ $EUID -eq 0 ]] || die "pre-mutation recovery must run as root"

for required_command in \
    awk bash basename chmod chown cmp cut dirname find flock getent grep id install \
    mkdir mktemp mv readlink rm rmdir seq sha256sum sleep sort stat sync systemctl tar tr wc xargs; do
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

validate_trusted_recovery_release() {
    local canonical relative entry owner mode permissions version commit tree helper
    canonical=$(readlink -e -- "$TRUSTED_RELEASE_ROOT") \
        || die "trusted recovery release root is unavailable"
    [[ "$canonical" == "$TRUSTED_RELEASE_ROOT" &&
       "$canonical" == "$RELEASES_ROOT/"* ]] \
        || die "trusted recovery release is aliased or outside release storage"
    relative=${canonical#"$RELEASES_ROOT/"}
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ &&
       "${relative:0:12}" == "${RECOVERY_COMMIT:0:12}" ]] \
        || die "trusted recovery release basename is not canonical or commit-bound"
    validate_root_trusted_dir_chain "$canonical"
    read -r owner mode < <(stat -Lc '%u %a' -- "$canonical") \
        || die "cannot inspect trusted recovery release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] \
        || die "trusted recovery release root must be root-owned mode 0700"
    if find "$canonical" -type l -print -quit | grep -q .; then
        die "trusted recovery release contains a symbolic link"
    fi
    if find "$canonical" ! -type d ! -type f -print -quit | grep -q .; then
        die "trusted recovery release contains a special filesystem object"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") \
            || die "cannot inspect trusted recovery release entry: $entry"
        [[ "$owner" == 0 ]] \
            || die "trusted recovery release entry must be root-owned: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "trusted recovery release entry is group/other writable: $entry"
    done < <(find "$canonical" -mindepth 1 -print0)
    [[ ! -e "$canonical/.git" && ! -L "$canonical/.git" ]] \
        || die "trusted recovery release must be an immutable archive"
    [[ -f "$canonical/SHA256SUMS" && ! -L "$canonical/SHA256SUMS" ]] \
        || die "trusted recovery release checksum manifest is missing"
    (
        cd "$canonical"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "trusted recovery release checksum verification failed"
    [[ -f "$canonical/release.version" && ! -L "$canonical/release.version" &&
       -f "$canonical/release.commit" && ! -L "$canonical/release.commit" &&
       -f "$canonical/release.tree" && ! -L "$canonical/release.tree" ]] \
        || die "trusted recovery release provenance is incomplete"
    version=$(tr -d '[:space:]' < "$canonical/release.version")
    commit=$(tr -d '[:space:]' < "$canonical/release.commit")
    tree=$(tr -d '[:space:]' < "$canonical/release.tree")
    [[ "$version" == 1 && "$commit" == "$RECOVERY_COMMIT" ]] \
        || die "trusted recovery release provenance does not match"
    [[ "$tree" =~ ^[0-9a-f]{40}$ || "$tree" =~ ^[0-9a-f]{64}$ ]] \
        || die "trusted recovery release tree is not canonical"
    for helper in release-transaction-guard.sh release-transaction-start-guard.sh \
        abort-pre-mutation-active-update.sh; do
        [[ -f "$canonical/deploy/$helper" && ! -L "$canonical/deploy/$helper" ]] \
            || die "trusted recovery helper is missing or unsafe: $helper"
    done
    helper=$(readlink -e -- "$0") || die "cannot resolve running recovery helper"
    [[ "$helper" == "$canonical/deploy/abort-pre-mutation-active-update.sh" ]] \
        || die "pre-mutation recovery helper must execute from the verified trusted release"
}

cleanup_verification_tmp() {
    [[ -n "${verification_tmp:-}" ]] || return 0
    case "$verification_tmp" in
        /run/celikpanel-pre-mutation-verify.*) ;;
        *) die "refusing to clean an unexpected verification directory" ;;
    esac
    if [[ -e "$verification_tmp" || -L "$verification_tmp" ]]; then
        [[ -d "$verification_tmp" && ! -L "$verification_tmp" ]] \
            || die "verification directory became unsafe"
        rm -rf -- "$verification_tmp" \
            || die "cannot remove the temporary web verification tree"
    fi
    verification_tmp=
}

validate_previous_release_file() {
    local path=$1 label=$2 owner group mode links
    [[ -f "$path" && ! -L "$path" ]] \
        || die "$label is missing or unsafe"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") \
        || die "cannot inspect $label"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 644 && "$links" == 1 ]] \
        || die "$label must be root:root mode 0644 with one link"
}

verify_installed_web_matches_previous_release() {
    local archive=$1 entries entry owner group mode links permissions archive_entries
    validate_root_trusted_dir_chain /run
    validate_root_trusted_dir_chain "$WEB_DIR"
    [[ -d "$WEB_DIR" && ! -L "$WEB_DIR" ]] \
        || die "installed web root is missing or unsafe"
    if find "$WEB_DIR" -xdev -type l -print -quit | grep -q .; then
        die "installed web tree contains a symbolic link"
    fi
    if find "$WEB_DIR" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "installed web tree contains a special filesystem object"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$entry") \
            || die "cannot inspect installed web object"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "installed web objects must be root:root"
        (( (permissions & 0022) == 0 )) \
            || die "installed web objects must not be group/other writable"
        [[ -d "$entry" || "$links" == 1 ]] \
            || die "installed web files must have one link"
    done < <(find "$WEB_DIR" -xdev -mindepth 1 -print0)

    archive_entries=$(tar --list --gzip --file="$archive") \
        || die "cannot list the previous web archive"
    [[ -n "$archive_entries" ]] \
        || die "previous web archive is empty"
    while IFS= read -r entry; do
        [[ "$entry" == . || "$entry" == ./ || "$entry" == ./* ]] \
            || die "previous web archive member is not rooted at the reviewed tree"
        [[ "$entry" != /* && "$entry" != ../* && "$entry" != *'/../'* &&
           "$entry" != */.. && "$entry" != *$'\t'* && "$entry" != *$'\r'* ]] \
            || die "previous web archive contains an unsafe member name"
    done <<< "$archive_entries"

    verification_tmp=$(mktemp -d -p /run 'celikpanel-pre-mutation-verify.XXXXXXXXXX') \
        || die "cannot create a root-only web verification directory"
    chown root:root -- "$verification_tmp"
    chmod 0700 -- "$verification_tmp"
    tar --extract --gzip --file="$archive" --directory="$verification_tmp" \
        --no-same-owner --no-same-permissions \
        || die "cannot extract the previous web archive for verification"
    [[ "$(readlink -e -- "$verification_tmp")" == "$verification_tmp" ]] \
        || die "temporary web verification directory is not canonical"
    if find "$verification_tmp" -xdev -type l -print -quit | grep -q .; then
        die "previous web archive expands to a symbolic link"
    fi
    if find "$verification_tmp" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "previous web archive expands to a special filesystem object"
    fi
    entries=$(find "$verification_tmp" -mindepth 1 -maxdepth 1 -printf '%f\n' \
        | LC_ALL=C sort)
    [[ "$entries" == $'assets\nindex.html\nvite.svg' ]] \
        || die "previous web archive top-level allowlist differs from the reviewed release"
    [[ "$(find "$verification_tmp" -xdev -type d | wc -l)" -eq 2 &&
       "$(find "$verification_tmp" -xdev -type f | wc -l)" -eq 7 ]] \
        || die "previous web archive object count differs from the reviewed release"
    while IFS= read -r -d '' entry; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$entry") \
            || die "cannot inspect extracted previous web object"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "extracted previous web objects must be root:root"
        (( (permissions & 0022) == 0 )) \
            || die "extracted previous web objects must not be group/other writable"
        [[ -d "$entry" || "$links" == 1 ]] \
            || die "extracted previous web files must have one link"
    done < <(find "$verification_tmp" -xdev -mindepth 1 -print0)
    cmp -s \
        <(cd "$verification_tmp" && LC_ALL=C find . -mindepth 1 -printf '%y\t%p\n' | LC_ALL=C sort) \
        <(cd "$WEB_DIR" && LC_ALL=C find . -mindepth 1 -printf '%y\t%p\n' | LC_ALL=C sort) \
        || die "installed web tree structure differs from the previous release"
    cmp -s \
        <(cd "$verification_tmp" && LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
        <(cd "$WEB_DIR" && LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
        || die "installed web bytes differ from the previous release"
    cleanup_verification_tmp
}

validate_previous_release_and_installed_artifacts() {
    local canonical entries file actual
    PREVIOUS_RELEASE_ROOT=$PREVIOUS_RELEASES_ROOT/$PREVIOUS_RELEASE_COMMIT
    validate_root_trusted_dir_chain "$PREVIOUS_RELEASES_ROOT"
    canonical=$(readlink -e -- "$PREVIOUS_RELEASE_ROOT") \
        || die "reviewed previous release root is unavailable"
    [[ "$canonical" == "$PREVIOUS_RELEASE_ROOT" &&
       "$canonical" == "$PREVIOUS_RELEASES_ROOT/$PREVIOUS_RELEASE_COMMIT" ]] \
        || die "reviewed previous release root is aliased or not an exact direct child"
    validate_root_trusted_dir_chain "$canonical"
    [[ "$(stat -Lc '%u:%g:%a' -- "$canonical")" == 0:0:755 ]] \
        || die "reviewed previous release root must be root:root mode 0755"
    entries=$(find "$canonical" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    [[ "$entries" == $'SHA256SUMS\nagent\npanel\nweb.tar.gz' ]] \
        || die "reviewed previous release contains unexpected entries"
    [[ -z "$(find "$canonical" -mindepth 2 -print -quit)" ]] \
        || die "reviewed previous release must contain only direct regular files"
    for file in SHA256SUMS agent panel web.tar.gz; do
        validate_previous_release_file "$canonical/$file" "previous release $file"
    done
    cmp -s "$canonical/SHA256SUMS" <(printf '%s  agent\n%s  panel\n%s  web.tar.gz\n' \
        "$PREVIOUS_AGENT_SHA256" "$PREVIOUS_PANEL_SHA256" "$PREVIOUS_WEB_SHA256") \
        || die "previous release checksum manifest differs from the explicit reviewed hashes"
    (cd "$canonical" && sha256sum -c SHA256SUMS >/dev/null) \
        || die "reviewed previous release checksum verification failed"

    for file in agent panel; do
        [[ -f "$BIN_DIR/$file" && -x "$BIN_DIR/$file" && ! -L "$BIN_DIR/$file" ]] \
            || die "installed $file binary is missing or unsafe"
        [[ "$(stat -Lc '%u:%g:%a:%h' -- "$BIN_DIR/$file")" == 0:0:755:1 ]] \
            || die "installed $file binary metadata is unsafe"
        cmp -s "$canonical/$file" "$BIN_DIR/$file" \
            || die "installed $file bytes differ from the reviewed previous release"
    done
    actual=$(sha256sum "$BIN_DIR/agent" | awk '{print $1}')
    [[ "$actual" == "$PREVIOUS_AGENT_SHA256" ]] \
        || die "installed agent digest differs from the explicit reviewed digest"
    actual=$(sha256sum "$BIN_DIR/panel" | awk '{print $1}')
    [[ "$actual" == "$PREVIOUS_PANEL_SHA256" ]] \
        || die "installed panel digest differs from the explicit reviewed digest"
    actual=$(sha256sum "$canonical/web.tar.gz" | awk '{print $1}')
    [[ "$actual" == "$PREVIOUS_WEB_SHA256" ]] \
        || die "previous web archive digest differs from the explicit reviewed digest"
    verify_installed_web_matches_previous_release "$canonical/web.tar.gz"
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

load_exact_incident_service_state() {
    local ledger=$1 coordinators=$2 current_start
    cmp -s "$ledger" <(printf '%s\n' \
        $'celikpanel-agent.service\tenabled\tactive' \
        $'celikpanel-panel.service\tenabled\tactive' \
        $'celikpanel-firewall-restore.service\tnot-found\tinactive') \
        || die "service-state ledger differs from the reviewed pre-mutation incident"
    cmp -s "$coordinators" <(printf '%s\n' \
        $'celikpanel-agent.service\tactive\t748468\t121952692' \
        $'celikpanel-panel.service\tactive\t748470\t121952692') \
        || die "coordinator ledger differs from the reviewed pre-mutation incident"

    saved_enabled_states[celikpanel-agent.service]=enabled
    saved_enabled_states[celikpanel-panel.service]=enabled
    saved_enabled_states[celikpanel-firewall-restore.service]=not-found
    saved_active_states[celikpanel-agent.service]=active
    saved_active_states[celikpanel-panel.service]=active
    saved_active_states[celikpanel-firewall-restore.service]=inactive
    quiesce_active_states[celikpanel-agent.service]=active
    quiesce_active_states[celikpanel-panel.service]=active
    quiesce_main_pids[celikpanel-agent.service]=748468
    quiesce_main_pids[celikpanel-panel.service]=748470
    quiesce_start_times[celikpanel-agent.service]=121952692
    quiesce_start_times[celikpanel-panel.service]=121952692

    for unit in celikpanel-agent.service celikpanel-panel.service; do
        if current_start=$(coordinator_process_start_time "${quiesce_main_pids[$unit]}" 2>/dev/null); then
            [[ "$current_start" != "${quiesce_start_times[$unit]}" ]] \
                || die "captured pre-mutation coordinator process still exists: $unit"
        fi
    done
}

verify_saved_enablement() {
    local unit actual
    for unit in celikpanel-agent.service celikpanel-panel.service \
        celikpanel-firewall-restore.service; do
        actual=$(systemctl is-enabled "$unit" 2>/dev/null || true)
        [[ "${actual:-unknown}" == "${saved_enabled_states[$unit]}" ]] \
            || die "installed enablement differs from the reviewed service ledger: $unit"
    done
}

verify_saved_runtime_states() {
    systemctl is-active --quiet celikpanel-agent.service \
        || die "saved active agent was not restored"
    systemctl is-active --quiet celikpanel-panel.service \
        || die "saved active panel was not restored"
    if systemctl is-active --quiet celikpanel-firewall-restore.service; then
        die "saved absent firewall restore unit unexpectedly became active"
    fi
}

verify_exact_preledger_stage() {
    local found entries incomplete_entries created target nonce child
    release_txn_validate_update_snapshot_stage \
        "$SNAPSHOT_ROOT" "$ACTIVE_SNAPSHOT" "$EXPECTED_STAGE" \
        || die "expected snapshot stage failed canonical guard validation"
    found=$(release_txn_find_update_snapshot_stage "$SNAPSHOT_ROOT" "$ACTIVE_SNAPSHOT") \
        || die "cannot resolve the one canonical stage for the active snapshot"
    [[ "$found" == "$EXPECTED_STAGE" ]] \
        || die "canonical stage differs from the explicitly reviewed stage"
    [[ "$(readlink -e -- "$EXPECTED_STAGE")" == "$EXPECTED_STAGE" ]] \
        || die "explicitly reviewed stage is not canonical"
    incomplete_entries=$(find "$SNAPSHOT_ROOT" -mindepth 1 -maxdepth 1 \
        -name '.release-snapshot.incomplete*' -print | LC_ALL=C sort)
    [[ "$incomplete_entries" == "$EXPECTED_STAGE" ]] \
        || die "snapshot storage contains another incomplete release stage"
    [[ ! -e "$SNAPSHOT_ROOT/$ACTIVE_SNAPSHOT" &&
       ! -L "$SNAPSHOT_ROOT/$ACTIVE_SNAPSHOT" ]] \
        || die "final snapshot exists; pre-mutation abort is forbidden"
    IFS=$'\t' read -r created target nonce \
        < <(release_txn_parse_update_snapshot_name "$ACTIVE_SNAPSHOT") \
        || die "active snapshot name is not canonical"
    [[ "$target" == "$ACTIVE_TARGET" ]] \
        || die "active snapshot target differs from the explicit active target"
    child=$EXPECTED_STAGE/$ACTIVE_SNAPSHOT
    entries=$(find "$child" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    [[ "$entries" == $'quiesce-coordinators.tsv\nservice-states.tsv\nsnapshot-transition.state' ]] \
        || die "pre-ledger stage contains payload beyond the exact reviewed allowlist"
    [[ -z "$(find "$child" -mindepth 2 -print -quit)" ]] \
        || die "pre-ledger stage contains nested payload"
    cmp -s "$child/snapshot-transition.state" <(printf 'pre-ledger\n') \
        || die "snapshot transition is not the exact pre-ledger state"
    load_exact_incident_service_state \
        "$child/service-states.tsv" "$child/quiesce-coordinators.tsv"
}

capture_active_marker_evidence() {
    local marker_count=0 token operation snapshot marker
    for marker in quiesce.pending active completion.pending; do
        [[ -e "$TRANSACTION_ROOT/$marker" || -L "$TRANSACTION_ROOT/$marker" ]] &&
            marker_count=$((marker_count + 1))
    done
    [[ "$marker_count" -eq 1 &&
       ! -e "$TRANSACTION_ROOT/quiesce.pending" &&
       ! -L "$TRANSACTION_ROOT/quiesce.pending" &&
       -f "$TRANSACTION_ROOT/active" &&
       ! -L "$TRANSACTION_ROOT/active" &&
       ! -e "$TRANSACTION_ROOT/completion.pending" &&
       ! -L "$TRANSACTION_ROOT/completion.pending" ]] \
        || die "recovery requires active as the only durable transaction marker"
    IFS=$'\t' read -r token operation snapshot \
        < <(release_txn_read_active_fields "$TRANSACTION_ROOT") \
        || die "cannot read the exact active transaction marker"
    [[ "$token" == "$ACTIVE_TOKEN" && "$operation" == update &&
       "$snapshot" == "$ACTIVE_SNAPSHOT" ]] \
        || die "active marker differs from the explicitly reviewed incident"
    active_marker_identity=$(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/active") \
        || die "cannot identify the active marker inode"
    active_marker_digest=$(sha256sum "$TRANSACTION_ROOT/active" | awk '{print $1}')
    [[ "$active_marker_digest" =~ ^[0-9a-f]{64}$ ]] \
        || die "cannot digest the active marker"
}

verify_active_evidence() {
    local identity digest
    release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        || die "release transaction lock changed during recovery proof"
    release_txn_validate_active_token \
        "$TRANSACTION_ROOT" "$ACTIVE_TOKEN" update "$ACTIVE_SNAPSHOT" \
        || die "active marker changed during recovery proof"
    identity=$(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/active") \
        || die "cannot re-identify the active marker"
    digest=$(sha256sum "$TRANSACTION_ROOT/active" | awk '{print $1}')
    [[ "$identity" == "$active_marker_identity" && "$digest" == "$active_marker_digest" ]] \
        || die "active marker identity or bytes changed during recovery"
    verify_exact_preledger_stage
    validate_previous_release_and_installed_artifacts
    verify_saved_enablement
    verify_both_units_stopped
}

verify_no_transaction_marker() {
    local marker
    release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        || die "release transaction lock changed after active marker removal"
    for marker in quiesce.pending active completion.pending; do
        [[ ! -e "$TRANSACTION_ROOT/$marker" && ! -L "$TRANSACTION_ROOT/$marker" ]] \
            || die "transaction marker reappeared after pre-mutation abort: $marker"
    done
}

verify_markerless_pre_start_evidence() {
    verify_mutation_lock_held
    verify_no_transaction_marker
    verify_exact_preledger_stage
    validate_previous_release_and_installed_artifacts
    verify_saved_enablement
    verify_both_units_stopped
}

verify_markerless_restored_evidence() {
    verify_mutation_lock_held
    verify_no_transaction_marker
    verify_exact_preledger_stage
    validate_previous_release_and_installed_artifacts
    verify_saved_enablement
    verify_saved_runtime_states
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
    systemctl daemon-reload \
        || die "systemd daemon-reload failed after runtime-directory preservation"
    loaded_dropins=$(systemctl show --property=DropInPaths --value celikpanel-agent.service) \
        || die "cannot inspect loaded agent systemd drop-ins"
    found=0
    for loaded in $loaded_dropins; do
        [[ "$loaded" != "$target" ]] || found=1
    done
    [[ "$found" == 1 ]] \
        || die "systemd did not load the exact runtime-directory preserve drop-in"
    manager_value=$(systemctl show --property=RuntimeDirectoryPreserve --value celikpanel-agent.service) \
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

open_mutation_lock() {
    local expected_group owner group mode links size path_identity fd_identity
    expected_group=$(getent group celikpanel | cut -d: -f3) \
        || die "celikpanel group is unavailable"
    if [[ ! -e "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]]; then
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
    MUTATION_LOCK_IDENTITY=$path_identity
    exec {MUTATION_LOCK_FD}<>"$MUTATION_LOCK"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify opened mutation lock"
    [[ "$fd_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
        || die "opened mutation lock does not match the recorded inode"
    flock -n -x "$MUTATION_LOCK_FD" \
        || die "a service or package mutation is active"
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

verify_running_unit_executable() {
    local unit=$1
    local expected_path=$2
    local expected_sha256=$3
    local main_pid process_executable path_identity process_identity actual_sha256

    main_pid=$(systemctl show "$unit" --property=MainPID --value) \
        || die "cannot read MainPID for $unit"
    [[ "$main_pid" =~ ^[1-9][0-9]*$ ]] \
        || die "$unit has no live MainPID after the controlled start"
    [[ -e "/proc/$main_pid/exe" ]] \
        || die "$unit MainPID executable is unavailable"

    process_executable=$(readlink -e -- "/proc/$main_pid/exe") \
        || die "cannot resolve the executable for $unit MainPID $main_pid"
    [[ "$process_executable" == "$expected_path" ]] \
        || die "$unit is not running the reviewed installed executable"

    path_identity=$(stat -Lc '%d:%i' -- "$expected_path") \
        || die "cannot identify the reviewed installed executable for $unit"
    process_identity=$(stat -Lc '%d:%i' -- "/proc/$main_pid/exe") \
        || die "cannot identify the running executable for $unit"
    [[ "$process_identity" == "$path_identity" ]] \
        || die "$unit is running a different executable inode"

    actual_sha256=$(sha256sum "/proc/$main_pid/exe" | awk '{print $1}') \
        || die "cannot hash the running executable for $unit"
    [[ "$actual_sha256" == "$expected_sha256" ]] \
        || die "$unit running executable digest does not match the reviewed previous release"
}

wait_for_agent_ready() {
    local _
    verify_mutation_lock_held
    [[ ! -e "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]] \
        || die "agent socket exists before the controlled markerless start"
    systemctl start celikpanel-agent.service \
        || die "reviewed previous-release agent could not be started"
    for _ in $(seq 1 50); do
        if systemctl is-active --quiet celikpanel-agent.service &&
           [[ -S "$AGENT_SOCKET" && ! -L "$AGENT_SOCKET" ]]; then
            verify_mutation_lock_held
            verify_running_unit_executable \
                celikpanel-agent.service "$BIN_DIR/agent" \
                "$PREVIOUS_AGENT_SHA256"
            return 0
        fi
        sleep 0.2
    done
    die "reviewed previous-release agent did not become active with its exact socket"
}

wait_for_panel_ready() {
    local _
    verify_mutation_lock_held
    systemctl start celikpanel-panel.service \
        || die "reviewed previous-release panel could not be started"
    for _ in $(seq 1 50); do
        if systemctl is-active --quiet celikpanel-panel.service; then
            verify_mutation_lock_held
            verify_running_unit_executable \
                celikpanel-panel.service "$BIN_DIR/panel" \
                "$PREVIOUS_PANEL_SHA256"
            return 0
        fi
        sleep 0.2
    done
    die "reviewed previous-release panel did not become active"
}

recovery_exit() {
    local status=$?
    trap - EXIT
    if [[ -n "${verification_tmp:-}" &&
          "$verification_tmp" == /run/celikpanel-pre-mutation-verify.* &&
          -d "$verification_tmp" && ! -L "$verification_tmp" ]]; then
        rm -rf -- "$verification_tmp" >/dev/null 2>&1 || true
        verification_tmp=
    fi
    if [[ "$status" -ne 0 && "$recovery_succeeded" -eq 0 ]]; then
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
        if [[ "$marker_removal_begun" -eq 1 &&
              ! -e "$TRANSACTION_ROOT/active" &&
              ! -L "$TRANSACTION_ROOT/active" ]]; then
            printf '%s\n' \
                "!! Pre-mutation abort failed closed after active marker removal; both coordinators were stopped and the stage was left for inspection." >&2
        elif [[ -e "$TRANSACTION_ROOT/active" || -L "$TRANSACTION_ROOT/active" ]]; then
            printf '%s\n' \
                "!! Pre-mutation abort failed closed; active marker and stage were preserved and both coordinators remain stopped." >&2
        else
            printf '%s\n' \
                "!! Pre-mutation abort failed closed with no active marker; both coordinators were stopped for manual inspection." >&2
        fi
    fi
    return "$status"
}

acquire_release_transaction_lock
validate_trusted_recovery_release

# Only the verified immutable recovery release supplies the parser, persistent
# systemd guard and terminal active-marker primitive used below.
# shellcheck source=deploy/release-transaction-guard.sh
source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"
release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "verified recovery guard rejected the inherited transaction lock"

capture_active_marker_evidence
verify_exact_preledger_stage
verify_saved_enablement
verify_both_units_stopped
trap recovery_exit EXIT
validate_previous_release_and_installed_artifacts

# Install the persistent start guard before touching the marker. It blocks any
# out-of-band start while active exists and permits the controlled ordinary
# starts only after the durable marker removal below.
install_and_verify_runtime_directory_preserve
release_txn_install_and_verify_unit_guards \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$TRANSACTION_START_HELPER" "$RELEASE_TRANSACTION_FD" \
    || die "cannot install persistent release-transaction start guards"
release_txn_clear_stale_start_authorization \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "stale start authorization could not be cleared"
verify_active_evidence

prepare_runtime_mutation_lock_dir
install_and_verify_runtime_directory_preserve
release_txn_install_and_verify_unit_guards \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$TRANSACTION_START_HELPER" "$RELEASE_TRANSACTION_FD" \
    || die "release-transaction start guards changed during lock preparation"
release_txn_clear_stale_start_authorization \
    "$TRANSACTION_ROOT" "$TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" \
    || die "stale start authorization reappeared during lock preparation"
verify_active_evidence

open_mutation_lock
verify_mutation_lock_held
remove_stale_agent_socket_under_lock
verify_mutation_lock_held
verify_active_evidence

# Terminal ordering is deliberate: the exact active marker is removed and its
# directory fsynced first. Only then may either coordinator accept traffic.
# A crash in between leaves a markerless outage, never a stale active marker
# hiding already-restored services.
marker_removal_begun=1
release_txn_remove_pre_mutation_active_marker \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$ACTIVE_TOKEN" update "$ACTIVE_SNAPSHOT" \
    "$SNAPSHOT_ROOT" "$EXPECTED_STAGE" \
    || die "cannot durably remove the exact pre-mutation active marker"
verify_markerless_pre_start_evidence

wait_for_agent_ready
verify_no_transaction_marker
verify_exact_preledger_stage
validate_previous_release_and_installed_artifacts
verify_saved_enablement
wait_for_panel_ready
verify_markerless_restored_evidence

# The stage is evidence until both saved runtime states and all reviewed bytes
# are restored. Cleanup is the last mutating operation and is delegated to the
# canonical markerless-stage guard while both coordination locks remain held.
release_txn_cleanup_unmarked_update_snapshot_stage \
    "$TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" "$SNAPSHOT_ROOT" \
    || die "cannot durably remove the exact markerless pre-ledger stage"
[[ ! -e "$EXPECTED_STAGE" && ! -L "$EXPECTED_STAGE" ]] \
    || die "markerless pre-ledger stage remains after guarded cleanup"
[[ ! -e "$SNAPSHOT_ROOT/$ACTIVE_SNAPSHOT" &&
   ! -L "$SNAPSHOT_ROOT/$ACTIVE_SNAPSHOT" ]] \
    || die "a final snapshot appeared during terminal cleanup"
[[ -z "$(find "$SNAPSHOT_ROOT" -mindepth 1 -maxdepth 1 \
    -name '.release-snapshot.incomplete*' -print -quit)" ]] \
    || die "an incomplete release stage remains after terminal cleanup"

recovery_succeeded=1
printf '%s\n' \
    "Pre-mutation active update aborted safely: exact previous release is running, active marker and pre-ledger stage are durably absent."
