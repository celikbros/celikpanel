#!/bin/bash
# CelikPanel rollback restores one complete snapshot produced by update.sh.
# Nothing is stopped or overwritten until the snapshot contract and every
# checksum have been verified.
#
# CelikPanel geri alma, update.sh tarafından üretilmiş tek bir eksiksiz
# snapshot'ı geri yükler. Snapshot sözleşmesi ve bütün checksum'lar doğrulanana
# kadar hiçbir şey durdurulmaz veya üzerine yazılmaz.
set -euo pipefail

# Ignore caller-controlled command lookup before privileged rollback.
# Ayrıcalıklı geri almadan önce çağıranın denetlediği komut arama yolunu yok say.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

SUPPORTED_SNAPSHOT_VERSION=6
SNAP_ROOT=/var/backups/celikpanel/update-snapshots
RELEASES_ROOT=/var/backups/celikpanel/releases
PREFIX=/opt/celikpanel
DATA_DIR=/var/lib/celikpanel
IMPORT_DIR=/var/lib/celikpanel-imports
CONF_DIR=/etc/celikpanel
PANEL_DB=/var/lib/celikpanel/celikpanel.db
BIN_DIR=/opt/celikpanel/bin
WEB_DIR=/opt/celikpanel/web
UNIT_DIR=/etc/systemd/system
AGENT_STATE_DIR=/var/lib/celikpanel-agent-private
AGENT_LEDGER="$AGENT_STATE_DIR/service-mutations.json"
PANEL_TLS_DIR=/var/lib/celikpanel/tls
PANEL_CERT_PENDING="$AGENT_STATE_DIR/panel-certificate-activation.json"
PANEL_CERT_HOOK=/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
MUTATION_LOCK_FD=
MUTATION_LOCK_IDENTITY=
RUNTIME_DIR=/run/celikpanel
BACKUP_ROOT=/var/backups/celikpanel
RELEASE_TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
RELEASE_TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
RELEASE_TRANSACTION_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
LIBEXEC_DIR=/usr/libexec/celikpanel
RELEASE_UPDATER=/usr/libexec/celikpanel/get.sh
readonly PREFIX DATA_DIR IMPORT_DIR CONF_DIR UNIT_DIR PANEL_CERT_HOOK \
    AGENT_STATE_DIR RUNTIME_DIR BACKUP_ROOT RELEASE_TRANSACTION_ROOT \
    RELEASE_TRANSACTION_RUNTIME_ROOT RELEASE_TRANSACTION_HELPER LIBEXEC_DIR RELEASE_UPDATER
SELINUX_OS_RELEASE=/etc/os-release
SELINUX_ENFORCE_FILE=/sys/fs/selinux/enforce
RHEL_DNF_BIN=/usr/bin/dnf
RHEL_DNF_CANONICAL_ALT=/usr/bin/dnf-3
RHEL_RPM_BIN=/usr/bin/rpm
APT_GET_BIN=/usr/bin/apt-get
APT_CACHE_BIN=/usr/bin/apt-cache
DPKG_QUERY_BIN=/usr/bin/dpkg-query
PACMAN_BIN=/usr/bin/pacman
TIMEOUT_BIN=/usr/bin/timeout
SELINUX_RESTORECON_BIN=/usr/sbin/restorecon
SELINUX_MATCHPATHCON_BIN=/usr/sbin/matchpathcon
SELINUX_GETENFORCE_BIN=/usr/sbin/getenforce
UNAME_BIN=/usr/bin/uname
# Bootstrap trust boundary: these fixed inspection helpers perform the first
# metadata read, so they cannot recursively attest themselves. They are never
# selected through PATH; replacing them already requires root-equivalent
# control of the vendor filesystem that this privileged script must trust.
VENDOR_READLINK_BIN=/usr/bin/readlink
VENDOR_STAT_BIN=/usr/bin/stat
VENDOR_DIRNAME_BIN=/usr/bin/dirname
SYSTEMCTL_BIN=/usr/bin/systemctl
SYSTEMD_RUNTIME_DIR=/run/systemd
SYSTEMD_PRIVATE_SOCKET=/run/systemd/private
VENDOR_TRUST_ANCHOR=/
VENDOR_EXPECTED_UID=0
VENDOR_EXPECTED_GID=0
readonly SELINUX_OS_RELEASE SELINUX_ENFORCE_FILE RHEL_DNF_BIN \
    RHEL_DNF_CANONICAL_ALT RHEL_RPM_BIN APT_GET_BIN APT_CACHE_BIN \
    DPKG_QUERY_BIN PACMAN_BIN TIMEOUT_BIN SELINUX_RESTORECON_BIN \
    SELINUX_MATCHPATHCON_BIN SELINUX_GETENFORCE_BIN UNAME_BIN VENDOR_READLINK_BIN \
    VENDOR_STAT_BIN VENDOR_DIRNAME_BIN SYSTEMCTL_BIN SYSTEMD_RUNTIME_DIR \
    SYSTEMD_PRIVATE_SOCKET VENDOR_TRUST_ANCHOR \
    VENDOR_EXPECTED_UID VENDOR_EXPECTED_GID
SELINUX_PLATFORM_MODE=unverified
PREFLIGHT_PANEL=
PREFLIGHT_AGENT=
PREFLIGHT_SCHEMA17_BRIDGE=
trusted_rollback_release_commit=
trusted_rollback_release_tree=
legacy_agent_frozen=0
rollback_mutation_started=0
rollback_transaction_started=0
rollback_transaction_token=
rollback_pending_resume=0
rollback_scheduler_only_resume=0
rollback_scheduler_restore_pending=0
rollback_scheduler_restore_completed=0
rollback_completion_verified=0
rollback_completion_removing=0
rollback_pending_snapshot=
rollback_verified_snapshot=
rollback_service_state_recorded=0
rollback_panel_was_active=0
rollback_agent_was_active=0
TRUSTED_RELEASE_ROOT=

die() {
    echo "!! $*" >&2
    exit 1
}

validate_vendor_directory_chain() {
    local path=$1 current parent canonical owner group mode permissions
    current=$("$VENDOR_DIRNAME_BIN" -- "$path") \
        || die "cannot derive vendor tool parent: $path"
    while true; do
        case "$VENDOR_TRUST_ANCHOR" in
            /) [[ "$current" == /* ]] || die "vendor tool path escaped root: $path" ;;
            *)
                [[ "$current" == "$VENDOR_TRUST_ANCHOR" || \
                   "$current" == "$VENDOR_TRUST_ANCHOR"/* ]] \
                    || die "vendor tool path escaped test trust anchor: $path"
                ;;
        esac
        [[ -d "$current" && ! -L "$current" ]] \
            || die "vendor tool ancestor is missing or symbolic: $current"
        canonical=$("$VENDOR_READLINK_BIN" -e -- "$current") \
            || die "cannot canonicalize vendor tool ancestor: $current"
        [[ "$canonical" == "$current" ]] \
            || die "vendor tool ancestor is not canonical: $current"
        read -r owner group mode < <("$VENDOR_STAT_BIN" -Lc '%u %g %a' -- "$current") \
            || die "cannot inspect vendor tool ancestor: $current"
        [[ "$owner" == "$VENDOR_EXPECTED_UID" && "$group" == "$VENDOR_EXPECTED_GID" ]] \
            || die "vendor tool ancestor is not owned by the trusted principal: $current"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "vendor tool ancestor is group/other writable: $current"
        [[ "$current" == "$VENDOR_TRUST_ANCHOR" ]] && break
        parent=$("$VENDOR_DIRNAME_BIN" -- "$current") \
            || die "cannot walk vendor tool ancestors: $current"
        [[ "$parent" != "$current" ]] \
            || die "vendor tool trust anchor was not reached: $path"
        current=$parent
    done
}

vendor_tool_path() {
    local role=$1
    case "$role" in
        uname) VENDOR_TOOL_PATH=$UNAME_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        systemctl) VENDOR_TOOL_PATH=$SYSTEMCTL_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        timeout) VENDOR_TOOL_PATH=$TIMEOUT_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        apt-get) VENDOR_TOOL_PATH=$APT_GET_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        apt-cache) VENDOR_TOOL_PATH=$APT_CACHE_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        dpkg-query) VENDOR_TOOL_PATH=$DPKG_QUERY_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        pacman) VENDOR_TOOL_PATH=$PACMAN_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        dnf) VENDOR_TOOL_PATH=$RHEL_DNF_BIN; VENDOR_TOOL_ALLOWED_ALT=$RHEL_DNF_CANONICAL_ALT ;;
        rpm) VENDOR_TOOL_PATH=$RHEL_RPM_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        restorecon) VENDOR_TOOL_PATH=$SELINUX_RESTORECON_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        matchpathcon) VENDOR_TOOL_PATH=$SELINUX_MATCHPATHCON_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        getenforce) VENDOR_TOOL_PATH=$SELINUX_GETENFORCE_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
        *) die "unknown vendor tool role: $role" ;;
    esac
}

vendor_tool_present() {
    vendor_tool_path "$1"
    [[ -e "$VENDOR_TOOL_PATH" || -L "$VENDOR_TOOL_PATH" ]]
}

validate_vendor_tool() {
    local role=$1 path canonical allowed_alt owner group mode links permissions
    vendor_tool_path "$role"
    path=$VENDOR_TOOL_PATH
    allowed_alt=$VENDOR_TOOL_ALLOWED_ALT
    [[ -e "$path" || -L "$path" ]] \
        || die "CelikPanel lifecycle requires the exact vendor $role path: $path"
    validate_vendor_directory_chain "$path"
    canonical=$("$VENDOR_READLINK_BIN" -e -- "$path") \
        || die "cannot resolve vendor $role path: $path"
    if [[ -L "$path" ]]; then
        # Only the vendor dnf compatibility link is accepted, and its resolved
        # target is pinned below to the reviewed /usr/bin/dnf-3 alternative.
        [[ "$role" == dnf ]] \
            || die "vendor $role path must not be symbolic: $path"
        read -r owner group < <("$VENDOR_STAT_BIN" -c '%u %g' -- "$path") \
            || die "cannot inspect vendor $role symlink: $path"
        [[ "$owner" == "$VENDOR_EXPECTED_UID" && "$group" == "$VENDOR_EXPECTED_GID" ]] \
            || die "vendor $role symlink is not owned by the trusted principal: $path"
    else
        [[ "$canonical" == "$path" ]] \
            || die "vendor $role path is not canonical: $path"
    fi
    [[ "$canonical" == "$path" || \
       ( -n "$allowed_alt" && "$canonical" == "$allowed_alt" ) ]] \
        || die "vendor $role canonical target is not pinned: $canonical"
    validate_vendor_directory_chain "$canonical"
    [[ -f "$canonical" && -x "$canonical" ]] \
        || die "vendor $role target is not an executable regular file: $canonical"
    read -r owner group mode links < <("$VENDOR_STAT_BIN" -Lc '%u %g %a %h' -- "$canonical") \
        || die "cannot inspect vendor $role target: $canonical"
    [[ "$owner" == "$VENDOR_EXPECTED_UID" && "$group" == "$VENDOR_EXPECTED_GID" ]] \
        || die "vendor $role target is not owned by the trusted principal: $canonical"
    [[ "$links" == 1 ]] \
        || die "vendor $role target must have exactly one hard link: $canonical"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) \
        || die "vendor $role target is group/other writable: $canonical"
}

validate_systemd_runtime() {
    local canonical owner group mode links permissions readiness readiness_status=0
    validate_vendor_tool systemctl
    validate_vendor_tool timeout
    [[ "$SYSTEMD_PRIVATE_SOCKET" == "$SYSTEMD_RUNTIME_DIR/private" ]] \
        || die "systemd private socket path does not match its fixed runtime directory"
    [[ -d "$SYSTEMD_RUNTIME_DIR" && ! -L "$SYSTEMD_RUNTIME_DIR" ]] \
        || die "systemd runtime directory is missing or symbolic: $SYSTEMD_RUNTIME_DIR"
    validate_vendor_directory_chain "$SYSTEMD_PRIVATE_SOCKET"
    [[ -S "$SYSTEMD_PRIVATE_SOCKET" && ! -L "$SYSTEMD_PRIVATE_SOCKET" ]] \
        || die "systemd private endpoint is not a direct Unix socket: $SYSTEMD_PRIVATE_SOCKET"
    canonical=$("$VENDOR_READLINK_BIN" -e -- "$SYSTEMD_PRIVATE_SOCKET") \
        || die "cannot canonicalize systemd private socket: $SYSTEMD_PRIVATE_SOCKET"
    [[ "$canonical" == "$SYSTEMD_PRIVATE_SOCKET" ]] \
        || die "systemd private socket is not canonical: $SYSTEMD_PRIVATE_SOCKET"
    read -r owner group mode links < <("$VENDOR_STAT_BIN" -Lc '%u %g %a %h' -- "$SYSTEMD_PRIVATE_SOCKET") \
        || die "cannot inspect systemd private socket: $SYSTEMD_PRIVATE_SOCKET"
    [[ "$owner" == "$VENDOR_EXPECTED_UID" && "$group" == "$VENDOR_EXPECTED_GID" ]] \
        || die "systemd private socket is not owned by the trusted principal"
    [[ "$links" == 1 ]] \
        || die "systemd private socket must have exactly one hard link"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) \
        || die "systemd private socket is group/other writable"

    readiness=$(LC_ALL=C SYSTEMD_COLORS=0 SYSTEMD_PAGER= \
        "$TIMEOUT_BIN" --signal=KILL --kill-after=1s 3s \
        "$SYSTEMCTL_BIN" is-system-running 2>/dev/null) || readiness_status=$?
    case "$readiness_status:$readiness" in
        0:running|0:degraded|1:degraded) ;;
        *) die "systemd is not ready (state=${readiness:-unknown}, status=$readiness_status)" ;;
    esac
}

validate_rhel_vendor_tool() {
    validate_vendor_tool "$1"
}

validate_present_platform_tools() {
    local role
    validate_systemd_runtime
    for role in apt-get apt-cache dpkg-query pacman dnf rpm; do
        if vendor_tool_present "$role"; then
            validate_vendor_tool "$role"
        fi
    done
}

package_ecosystem_complete() {
    case "$1" in
        apt)
            vendor_tool_present apt-get &&
                vendor_tool_present apt-cache &&
                vendor_tool_present dpkg-query
            ;;
        pacman) vendor_tool_present pacman ;;
        dnf) vendor_tool_present dnf && vendor_tool_present rpm ;;
        *) return 1 ;;
    esac
}

validate_selected_package_ecosystem() {
    local family=$1 role
    [[ "$family" != dnf-preview ]] || family=dnf
    case "$family" in
        apt) set -- apt-get apt-cache dpkg-query ;;
        pacman) set -- pacman ;;
        dnf) set -- dnf rpm ;;
        *) die "unknown package ecosystem: $family" ;;
    esac
    for role in "$@"; do
        validate_vendor_tool "$role"
    done
}

vendor_machine_architecture() {
    local machine
    validate_rhel_vendor_tool uname
    machine=$("$UNAME_BIN" -m) || die "cannot determine vendor machine architecture"
    printf '%s\n' "$machine"
}

parse_lifecycle_os_release_scalar() {
    local raw=$1 field=$2 value first last
    first=${raw:0:1}
    last=${raw: -1}
    if [[ "$first" == '"' ]]; then
        [[ ${#raw} -ge 2 && "$last" == '"' ]] \
            || die "malformed quoted $field in operating-system identity file"
        value=${raw:1:${#raw}-2}
        [[ "$value" != *'"'* && "$value" != *\\* ]] \
            || die "unsupported escape in $field in operating-system identity file"
    elif [[ "$first" == "'" ]]; then
        [[ ${#raw} -ge 2 && "$last" == "'" ]] \
            || die "malformed quoted $field in operating-system identity file"
        value=${raw:1:${#raw}-2}
        [[ "$value" != *"'"* && "$value" != *\\* ]] \
            || die "unsupported escape in $field in operating-system identity file"
    else
        [[ "$raw" != *'"'* && "$raw" != *"'"* && "$raw" != *\\* ]] \
            || die "malformed $field in operating-system identity file"
        value=$raw
    fi
    LIFECYCLE_OS_RELEASE_VALUE=$value
}

parse_lifecycle_os_release() {
    local file=$1 line key raw
    local -A seen=()
    LIFECYCLE_DISTRO_ID=
    LIFECYCLE_DISTRO_VERSION_ID=
    LIFECYCLE_DISTRO_ID_LIKE=
    [[ -f "$file" && -r "$file" ]] \
        || die "missing operating-system identity file: $file"
    if IFS= read -r -d '' _ < "$file"; then
        die "NUL byte in operating-system identity file: $file"
    fi
    while IFS= read -r line || [[ -n "$line" ]]; do
        case "$line" in ''|'#'*) continue ;; esac
        [[ "$line" =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] \
            || die "malformed entry in operating-system identity file: $file"
        key=${BASH_REMATCH[1]}
        raw=${BASH_REMATCH[2]}
        case "$key" in
            ID|VERSION_ID|ID_LIKE)
                [[ -z "${seen[$key]+present}" ]] \
                    || die "duplicate $key in operating-system identity file: $file"
                seen[$key]=1
                parse_lifecycle_os_release_scalar "$raw" "$key"
                case "$key" in
                    ID) LIFECYCLE_DISTRO_ID=$LIFECYCLE_OS_RELEASE_VALUE ;;
                    VERSION_ID) LIFECYCLE_DISTRO_VERSION_ID=$LIFECYCLE_OS_RELEASE_VALUE ;;
                    ID_LIKE) LIFECYCLE_DISTRO_ID_LIKE=$LIFECYCLE_OS_RELEASE_VALUE ;;
                esac
                ;;
        esac
    done < "$file"
    [[ "$LIFECYCLE_DISTRO_ID" =~ ^[a-z0-9][a-z0-9._-]*$ ]] \
        || die "missing or invalid ID in operating-system identity file: $file"
    [[ -z "$LIFECYCLE_DISTRO_VERSION_ID" ||
       "$LIFECYCLE_DISTRO_VERSION_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._:+-]*$ ]] \
        || die "invalid VERSION_ID in operating-system identity file: $file"
    [[ -z "$LIFECYCLE_DISTRO_ID_LIKE" ||
       "$LIFECYCLE_DISTRO_ID_LIKE" =~ ^[a-z0-9][a-z0-9._-]*(\ [a-z0-9][a-z0-9._-]*)*$ ]] \
        || die "invalid ID_LIKE in operating-system identity file: $file"
}

package_hint_for_token() {
    case "$1" in
        debian|ubuntu) printf '%s\n' apt ;;
        arch) printf '%s\n' pacman ;;
        rhel|fedora|centos|almalinux|rocky|rocky-linux|cloudlinux) printf '%s\n' dnf ;;
        *) return 1 ;;
    esac
}

select_lifecycle_package_ecosystem() {
    local token hint candidate selected= combined
    local -A hints=()
    local -a complete=()

    validate_present_platform_tools
    combined="$LIFECYCLE_DISTRO_ID $LIFECYCLE_DISTRO_ID_LIKE"
    for token in $combined; do
        hint=$(package_hint_for_token "$token") || continue
        hints[$hint]=1
    done
    for candidate in apt pacman dnf; do
        if package_ecosystem_complete "$candidate"; then
            complete+=("$candidate")
        fi
    done

    if ((${#hints[@]} == 1)); then
        for selected in "${!hints[@]}"; do :; done
        package_ecosystem_complete "$selected" \
            || die "os-release expects the $selected package ecosystem, but its exact vendor toolchain is incomplete"
    else
        ((${#complete[@]} == 1)) \
            || die "package ecosystem is missing or ambiguous; exactly one complete trusted toolchain is required"
        selected=${complete[0]}
    fi

    validate_selected_package_ecosystem "$selected"
    if [[ "$selected" == dnf ]]; then
        PKG_FAMILY=dnf-preview
        SELINUX_PLATFORM_MODE=dnf-preview
    else
        PKG_FAMILY=$selected
        SELINUX_PLATFORM_MODE=inert
    fi
}

classify_lifecycle_platform() {
    local os_release=$1 machine=$2 arch
    SELINUX_PLATFORM_MODE=unverified
    parse_lifecycle_os_release "$os_release"
    case "$machine" in
        x86_64) arch=amd64 ;;
        aarch64) arch=arm64 ;;
        *) die "unsupported rollback architecture: $machine" ;;
    esac
    select_lifecycle_package_ecosystem
}

verify_live_selinux_preflight() {
    local enforcing trailing= enforce_fd mode permissions
    if [[ -L "$SELINUX_ENFORCE_FILE" ]]; then
        die "SELinux enforcement state path must not be symbolic"
    fi
    if [[ ! -e "$SELINUX_ENFORCE_FILE" ]]; then
        return 0
    fi
    [[ -f "$SELINUX_ENFORCE_FILE" && -r "$SELINUX_ENFORCE_FILE" ]] ||
        die "SELinux enforcement state is unavailable or unreadable"
    mode=$("$VENDOR_STAT_BIN" -Lc '%a' -- "$SELINUX_ENFORCE_FILE") ||
        die "cannot inspect SELinux enforcement state metadata"
    [[ "$mode" =~ ^[0-7]{3,4}$ ]] ||
        die "SELinux enforcement state metadata is malformed"
    permissions=$((8#$mode))
    (( (permissions & 0444) != 0 )) ||
        die "SELinux enforcement state has no readable permission bit"
    if IFS= read -r -d '' _ < "$SELINUX_ENFORCE_FILE"; then
        die "SELinux enforcement state contains a NUL byte"
    fi
    exec {enforce_fd}<"$SELINUX_ENFORCE_FILE" ||
        die "cannot open SELinux enforcement state"
    if ! IFS= read -r -u "$enforce_fd" enforcing; then
        exec {enforce_fd}<&-
        die "SELinux enforcement state must be newline-terminated"
    fi
    if IFS= read -r -u "$enforce_fd" trailing || [[ -n "$trailing" ]]; then
        exec {enforce_fd}<&-
        die "SELinux enforcement state must contain exactly one line"
    fi
    exec {enforce_fd}<&-
    case "$enforcing" in
        0|1) ;;
        *) die "SELinux enforcement state is malformed" ;;
    esac
    [[ "$SELINUX_PLATFORM_MODE" == dnf-preview ]] ||
        die "SELinux is active but this package capability has no certified label lifecycle; no host changes were made"
}

verify_rhel_preview_host() {
    local enforcing reported_state
    [[ "$SELINUX_PLATFORM_MODE" == dnf-preview ]] \
        || die "DNF SELinux verification requires the DNF preview capability profile"
    [[ -f "$SELINUX_ENFORCE_FILE" && ! -L "$SELINUX_ENFORCE_FILE" && -r "$SELINUX_ENFORCE_FILE" ]] \
        || die "DNF rollback requires SELinux Enforcing; state is unavailable"
    IFS= read -r enforcing < "$SELINUX_ENFORCE_FILE" \
        || die "DNF rollback could not read SELinux enforcement state"
    [[ "$enforcing" == 1 ]] \
        || die "DNF rollback requires SELinux Enforcing"
    validate_rhel_vendor_tool dnf
    validate_rhel_vendor_tool rpm
    validate_rhel_vendor_tool restorecon
    validate_rhel_vendor_tool matchpathcon
    validate_rhel_vendor_tool getenforce
    reported_state=$("$SELINUX_GETENFORCE_BIN") \
        || die "DNF rollback could not query SELinux enforcement state"
    [[ "$reported_state" == Enforcing ]] \
        || die "DNF rollback requires getenforce to report Enforcing"
}

preflight_rollback_platform() {
    classify_lifecycle_platform "$1" "$2"
    verify_live_selinux_preflight
    if [[ "$SELINUX_PLATFORM_MODE" == dnf-preview ]]; then
        verify_rhel_preview_host
        die "DNF rollback remains preview-only: package capability is verified, but the SELinux lifecycle is not implemented; no host changes were made"
    fi
}

# SELinux lifecycle is inert only for the selected APT or pacman capability.
# DNF preview publication revalidates pinned vendor tools immediately before
# use and labels only fixed CelikPanel-owned paths.
restore_celikpanel_selinux_labels() {
    local state drift candidate
    local -a paths=()
    case "$SELINUX_PLATFORM_MODE" in
        inert) return 0 ;;
        dnf-preview) ;;
        *) die "SELinux lifecycle platform preflight was not completed" ;;
    esac
    validate_rhel_vendor_tool restorecon
    validate_rhel_vendor_tool matchpathcon
    validate_rhel_vendor_tool getenforce
    state=$("$SELINUX_GETENFORCE_BIN") \
        || die "SELinux lifecycle could not query enforcement state"
    [[ "$state" == Enforcing ]] \
        || die "SELinux lifecycle requires Enforcing mode and will not change host policy"

    for candidate in \
        "$PREFIX" \
        "$CONF_DIR" \
        "$DATA_DIR" \
        "$IMPORT_DIR" \
        "$AGENT_STATE_DIR" \
        "$RUNTIME_DIR" \
        "$BACKUP_ROOT" \
        "$RELEASE_TRANSACTION_ROOT" \
        "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
        "$LIBEXEC_DIR" \
        "$RELEASE_TRANSACTION_HELPER" \
        "$RELEASE_UPDATER" \
        "$PANEL_CERT_HOOK" \
        "$UNIT_DIR/celikpanel-agent.service" \
        "$UNIT_DIR/celikpanel-firewall-restore.service" \
        "$UNIT_DIR/celikpanel-panel.service" \
        "$UNIT_DIR/celikpanel-agent.service.d" \
        "$UNIT_DIR/celikpanel-panel.service.d" \
        "$UNIT_DIR/celikpanel-agent.service.d/10-release-transaction-guard.conf" \
        "$UNIT_DIR/celikpanel-panel.service.d/10-release-transaction-guard.conf"
    do
        if [[ -L "$candidate" ]]; then
            die "SELinux lifecycle refuses a symbolic-link publication root: $candidate"
        fi
        [[ -e "$candidate" ]] && paths+=("$candidate")
    done
    ((${#paths[@]} > 0)) || return 0

    "$SELINUX_RESTORECON_BIN" -xRF -- "${paths[@]}" \
        || die "CelikPanel SELinux labels could not be restored"
    drift=$("$SELINUX_RESTORECON_BIN" -nxRFv -- "${paths[@]}") \
        || die "CelikPanel SELinux labels could not be verified"
    [[ -z "$drift" ]] \
        || die "CelikPanel SELinux labels still differ from filesystem policy: $drift"
    for candidate in "${paths[@]}"; do
        "$SELINUX_MATCHPATHCON_BIN" -V -- "$candidate" >/dev/null \
            || die "CelikPanel SELinux top-level context differs from policy: $candidate"
    done
}

install_release_transaction_guards_with_label_barrier() {
    local status=0
    systemctl() {
        if [[ $# -eq 1 && "$1" == daemon-reload ]]; then
            restore_celikpanel_selinux_labels
        fi
        "$SYSTEMCTL_BIN" "$@"
    }
    release_txn_install_and_verify_unit_guards "$@" || status=$?
    unset -f systemctl
    systemctl() {
        "$SYSTEMCTL_BIN" "$@"
    }
    return "$status"
}

# Take the fixed persistent release lock before snapshot arguments, mutable
# state, or trusted-release bytes are read; keep its descriptor for the process.
# Snapshot argümanları, değişebilir state veya güvenilir-sürüm baytları okunmadan
# önce sabit kalıcı sürüm kilidini al; descriptor'ını süreç boyunca koru.
prepare_and_acquire_release_transaction_lock() {
    local root=$RELEASE_TRANSACTION_ROOT parent lock owner group mode links size
    local path_identity fd_identity
    parent=$(dirname -- "$root")
    [[ "$parent" == /var/lib && -d "$parent" && ! -L "$parent" ]] || die "unsafe release transaction parent: $parent"
    [[ "$(readlink -e -- "$parent")" == "$parent" ]] || die "release transaction parent is not canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$parent") || die "cannot inspect release transaction parent"
    [[ "$owner" == 0 && "$group" == 0 ]] || die "release transaction parent must be root-owned"
    (( (8#$mode & 0022) == 0 )) || die "release transaction parent must not be group/other writable"
    if [[ -e "$root" || -L "$root" ]]; then
        [[ -d "$root" && ! -L "$root" ]] || die "unsafe release transaction root"
    else
        install -d -m 0700 -o root -g root -- "$root" || die "cannot create release transaction root"
        sync -f -- "$parent" || die "cannot make release transaction root durable"
    fi
    [[ "$(readlink -e -- "$root")" == "$root" ]] || die "release transaction root is not canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$root") || die "cannot inspect release transaction root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] || die "release transaction root must be root:root mode 0700"
    lock=$root/transaction.lock
    if [[ -e "$lock" || -L "$lock" ]]; then
        [[ -f "$lock" && ! -L "$lock" ]] || die "unsafe release transaction lock"
    else
        (umask 077; set -o noclobber; : > "$lock") || die "cannot exclusively create release transaction lock"
        chown root:root -- "$lock" || die "cannot own release transaction lock"
        chmod 0600 -- "$lock" || die "cannot protect release transaction lock"
        sync -f -- "$lock" || die "cannot make release transaction lock durable"
        sync -f -- "$root" || die "cannot make release transaction lock entry durable"
    fi
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$lock") || die "cannot inspect release transaction lock"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 && "$size" == 0 ]] || die "release transaction lock must be empty root:root mode 0600 with one link"
    path_identity=$(stat -Lc '%d:%i' -- "$lock") || die "cannot identify release transaction lock"
    exec {RELEASE_TRANSACTION_FD}<>"$lock"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$RELEASE_TRANSACTION_FD") || die "cannot identify inherited release transaction lock"
    [[ "$fd_identity" == "$path_identity" ]] || die "release transaction lock changed while opening"
    flock -n "$RELEASE_TRANSACTION_FD" || die "another update or rollback transaction is active"
}

[[ $EUID -eq 0 ]] || die "Run as root / root olarak çalıştırın: use a trusted release rollback.sh"
rollback_machine=$(vendor_machine_architecture)
preflight_rollback_platform "$SELINUX_OS_RELEASE" "$rollback_machine"
# All existing lifecycle helpers call `systemctl`; bind that command name to
# the fixed path whose ownership and permissions preflight just verified.
systemctl() {
    "$SYSTEMCTL_BIN" "$@"
}
prepare_and_acquire_release_transaction_lock

# Every privileged path component must be root-owned and non-writable so a
# pathname cannot be redirected between verification and restore.
# Her ayrıcalıklı yol bileşeni root sahipli ve başkalarınca yazılamaz olmalıdır;
# böylece doğrulama ile geri yükleme arasında yol başka yere yönlendirilemez.
validate_root_trusted_dir_chain() {
    local path=$1 canonical current owner mode permissions
    [[ "$path" == /* ]] || die "trusted directory path must be absolute: $path"
    canonical=$(readlink -e -- "$path") || die "trusted directory is unavailable: $path"
    [[ "$canonical" == "$path" ]] || die "trusted directory path contains a symlink or alias: $path"
    current=$path
    while true; do
        [[ -d "$current" && ! -L "$current" ]] || die "unsafe trusted directory: $current"
        read -r owner mode < <(stat -Lc '%u %a' -- "$current") || die "cannot inspect trusted directory: $current"
        [[ "$owner" == 0 ]] || die "trusted directory must be owned by root: $current"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) || die "trusted directory must not be group/other writable: $current"
        [[ "$current" == / ]] && break
        current=$(dirname "$current")
    done
}

# Rollback itself is privileged code. It may execute only from a complete,
# root-owned immutable release staged by bootstrap-update.sh.
# Rollback kendisi ayrıcalıklı koddur. Yalnız bootstrap-update.sh tarafından
# hazırlanmış eksiksiz, root sahipli değişmez sürümden çalışabilir.
validate_running_release() {
    local script root relative entry owner mode permissions
    script=$(readlink -e -- "$0") || die "cannot resolve rollback entrypoint"
    root=$(dirname "$script")
    TRUSTED_RELEASE_ROOT=$root
    [[ "$root" == "$RELEASES_ROOT/"* ]] || die "rollback is outside trusted release storage"
    relative=${root#"$RELEASES_ROOT/"}
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] \
        || die "rollback release must be a canonical direct child"
    validate_root_trusted_dir_chain "$root"
    read -r owner mode < <(stat -Lc '%u %a' -- "$root") \
        || die "cannot inspect rollback release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] \
        || die "rollback release root must be root-owned mode 0700"
    if find "$root" -type l -print -quit | grep -q .; then
        die "rollback release contains a symbolic link"
    fi
    if find "$root" ! -type d ! -type f -print -quit | grep -q .; then
        die "rollback release contains a special filesystem object"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") \
            || die "cannot inspect rollback release entry: $entry"
        [[ "$owner" == 0 ]] || die "rollback release entry must be owned by root: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "rollback release entry must not be group/other writable: $entry"
    done < <(find "$root" -mindepth 1 -print0)
    [[ "$script" == "$root/rollback.sh" && -x "$script" && ! -L "$script" ]] \
        || die "rollback entrypoint is not the trusted release script"
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] \
        || die "rollback release checksum manifest is missing"
    (
        cd "$root"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "rollback release checksum verification failed"
    [[ -f "$root/release.version" && ! -L "$root/release.version" &&
       -f "$root/release.commit" && ! -L "$root/release.commit" &&
       -f "$root/release.tree" && ! -L "$root/release.tree" ]] \
        || die "rollback release provenance is incomplete"
    version=$(tr -d '[:space:]' < "$root/release.version")
    [[ "$version" == 1 ]] || die "unsupported rollback release version: $version"
    trusted_rollback_release_commit=$(tr -d '[:space:]' < "$root/release.commit")
    trusted_rollback_release_tree=$(tr -d '[:space:]' < "$root/release.tree")
    [[ "$trusted_rollback_release_commit" =~ ^[0-9a-f]{40,64}$ ]] \
        || die "invalid rollback release commit"
    [[ "$trusted_rollback_release_tree" =~ ^[0-9a-f]{40,64}$ ]] \
        || die "invalid rollback release tree"
    [[ "${trusted_rollback_release_commit:0:12}" == "${relative%%-*}" ]] \
        || die "rollback release directory does not match its commit"
    [[ -f "$root/deploy/panel-tls-snapshot.sh" &&
       ! -L "$root/deploy/panel-tls-snapshot.sh" ]] \
        || die "rollback release panel TLS snapshot helper is missing"
}

# Rollback preflights run as root. Accept only an absolute, root-owned,
# non-writable regular executable; verified staging binaries may be selected
# explicitly during the one-time pre-ledger transition.
# Geri alma ön kontrolleri root olarak çalışır. Yalnız mutlak yoldaki, root
# sahipli, yazılamayan normal executable kabul edilir; ledger öncesi tek seferlik
# geçişte doğrulanmış staging binary'leri açıkça seçilebilir.
validate_preflight_binary() {
    local path=$1 label=$2 owner mode permissions
    [[ "$path" == /* ]] || die "$label preflight binary path must be absolute: $path"
    validate_root_trusted_dir_chain "$(dirname "$path")"
    [[ -f "$path" && -x "$path" && ! -L "$path" ]] || die "$label preflight binary is unsafe or missing: $path"
    read -r owner mode < <(stat -Lc '%u %a' -- "$path") || die "cannot inspect $label preflight binary"
    [[ "$owner" == 0 ]] || die "$label preflight binary must be owned by root: $path"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) || die "$label preflight binary must not be group/other writable: $path"
}

# Enumerate every process in a systemd service cgroup, including delegated
# descendants. Missing inactive cgroups are treated as empty.
# Devredilmiş alt gruplar dahil systemd servis cgroup'undaki her işlemi listele.
# Etkin olmayan ve eksik cgroup boş kabul edilir.
service_cgroup_pids() {
    local unit=$1 control_group cgroup_root procs_file pid
    control_group=$(systemctl show --property=ControlGroup --value "$unit") \
        || die "cannot inspect control group for $unit"
    [[ -n "$control_group" ]] || return 0
    [[ "$control_group" == /* && "$control_group" != *'/../'* ]] \
        || die "unsafe control group for $unit: $control_group"
    cgroup_root="/sys/fs/cgroup$control_group"
    [[ -d "$cgroup_root" ]] || return 0
    while IFS= read -r -d '' procs_file; do
        while IFS= read -r pid; do
            [[ -z "$pid" || "$pid" =~ ^[0-9]+$ ]] || die "invalid pid in $procs_file"
            [[ -z "$pid" ]] || printf '%s\n' "$pid"
        done < "$procs_file"
    done < <(find "$cgroup_root" -type f -name cgroup.procs -print0)
}

# A pre-ledger agent may contain only its systemd MainPID. A helper process
# could still be mutating packages, so any extra process refuses rollback.
# Ledger öncesi agent yalnız systemd MainPID'ini içerebilir. Yardımcı bir işlem
# paketleri hâlâ değiştiriyor olabilir; bu yüzden her ek işlem geri almayı reddeder.
reject_extra_service_cgroup_processes() {
    local unit=$1 expected_main=$2 pid_output
    local -a pids=()
    pid_output=$(service_cgroup_pids "$unit") || die "cannot enumerate $unit control group"
    if [[ -n "$pid_output" ]]; then
        mapfile -t pids <<< "$pid_output"
    fi
    if [[ "$expected_main" -gt 1 ]]; then
        [[ ${#pids[@]} -eq 1 && "${pids[0]}" == "$expected_main" ]] \
            || die "$unit has extra or unexpected cgroup processes: ${pids[*]:-none}"
    else
        [[ ${#pids[@]} -eq 0 ]] || die "$unit has residual cgroup processes: ${pids[*]}"
    fi
}

unfreeze_legacy_agent() {
    if [[ $legacy_agent_frozen -eq 1 ]]; then
        systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-agent.service \
            >/dev/null 2>&1 || return 1
        legacy_agent_frozen=0
    fi
}

# An error after SIGSTOP must never make pre-ledger package mutation code
# runnable again. Queue a stop, kill the whole frozen cgroup without SIGCONT,
# and clear the frozen flag only after the unit is inactive and its cgroup is
# proved empty. This helper is intentionally bounded because it runs in EXIT.
# SIGSTOP sonrasındaki bir hata ledger öncesi paket mutasyon kodunu yeniden
# çalıştırmamalıdır. Durdurmayı sıraya al, donmuş cgroup'un tamamını SIGCONT
# olmadan öldür ve donmuş bayrağını yalnız birim pasif ve cgroup boş kanıtlanınca sil.
terminate_frozen_legacy_agent_fail_closed() {
    local pid_output
    [[ $legacy_agent_frozen -eq 1 ]] || return 0

    systemctl stop --no-block celikpanel-agent.service >/dev/null 2>&1 || true
    systemctl kill --kill-whom=all --signal=SIGKILL celikpanel-agent.service \
        >/dev/null 2>&1 || true
    systemctl stop --no-block celikpanel-agent.service >/dev/null 2>&1 || true

    for _ in $(seq 1 50); do
        if ! systemctl is-active --quiet celikpanel-agent.service; then
            pid_output=
            if pid_output=$(service_cgroup_pids celikpanel-agent.service 2>/dev/null) &&
               [[ -z "$pid_output" ]]; then
                legacy_agent_frozen=0
                return 0
            fi
        fi
        sleep 0.02
    done

    echo "!! Frozen pre-ledger agent cgroup could not be proved empty during fail-closed cleanup." >&2
    return 1
}

# Freeze before the cgroup proof so pre-ledger code cannot spawn a package
# command in the proof-to-stop gap. Then queue stop, kill the frozen cgroup
# without resuming it and require an empty cgroup before rollback continues.
# Ledger öncesi kod kanıt ile durdurma arasındaki boşlukta paket komutu
# başlatamasın diye önce dondur. Sonra durdurmayı sıraya al, donmuş cgroup'u
# sürdürmeden sonlandır ve geri alma devam etmeden önce boş cgroup zorunlu tut.
freeze_and_stop_legacy_agent() {
    local state main_pid frozen_state
    state=$(systemctl show --property=ActiveState --value celikpanel-agent.service) \
        || die "cannot inspect pre-ledger agent state"
    case "$state" in
        active)
            main_pid=$(systemctl show --property=MainPID --value celikpanel-agent.service) \
                || die "cannot inspect pre-ledger agent MainPID"
            [[ "$main_pid" =~ ^[0-9]+$ && "$main_pid" -gt 1 ]] \
                || die "pre-ledger agent has no valid MainPID"
            systemctl kill --kill-whom=all --signal=SIGSTOP celikpanel-agent.service \
                || die "pre-ledger agent could not be frozen"
            legacy_agent_frozen=1
            reject_extra_service_cgroup_processes celikpanel-agent.service "$main_pid"
            for _ in $(seq 1 50); do
                frozen_state=$(awk '/^State:/ {print $2}' "/proc/$main_pid/status" 2>/dev/null || true)
                [[ "$frozen_state" == T || "$frozen_state" == t ]] && break
                sleep 0.02
            done
            [[ "$frozen_state" == T || "$frozen_state" == t ]] \
                || die "pre-ledger agent did not enter a frozen state"
            systemctl stop --no-block celikpanel-agent.service \
                || die "pre-ledger agent stop could not be queued"
            systemctl kill --kill-whom=all --signal=SIGKILL celikpanel-agent.service \
                || die "frozen pre-ledger agent could not be terminated"
            systemctl stop celikpanel-agent.service \
                || die "pre-ledger agent could not be stopped"
            ;;
        inactive|failed)
            reject_extra_service_cgroup_processes celikpanel-agent.service 0
            systemctl stop celikpanel-agent.service \
                || die "pre-ledger agent could not be normalized to stopped"
            ;;
        *) die "pre-ledger agent is in an unstable state: $state" ;;
    esac
    systemctl is-active --quiet celikpanel-agent.service \
        && die "pre-ledger agent is still active after stop"
    reject_extra_service_cgroup_processes celikpanel-agent.service 0
    legacy_agent_frozen=0
}

# systemd removes RuntimeDirectory after agent shutdown. Recreate only the
# documented root:celikpanel 0750 directory required by the common flock.
# systemd agent kapandıktan sonra RuntimeDirectory'yi kaldırır. Ortak flock için
# yalnız belgelenmiş root:celikpanel 0750 dizinini yeniden oluştur.
prepare_runtime_mutation_lock_dir() {
    local lock_dir group_id owner group mode
    lock_dir=$(dirname "$MUTATION_LOCK")
    [[ "$lock_dir" == /run/celikpanel ]] || die "unexpected mutation lock directory: $lock_dir"
    group_id=$(getent group celikpanel | cut -d: -f3) || die "celikpanel group is unavailable"
    [[ "$group_id" =~ ^[0-9]+$ ]] || die "celikpanel group id is invalid"
    if [[ -e "$lock_dir" || -L "$lock_dir" ]]; then
        [[ -d "$lock_dir" && ! -L "$lock_dir" ]] || die "unsafe mutation lock directory"
        validate_root_trusted_dir_chain "$lock_dir"
    fi
    install -d -m 0750 -o root -g celikpanel -- "$lock_dir" \
        || die "cannot prepare mutation lock directory"
    validate_root_trusted_dir_chain "$lock_dir"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect prepared mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
}

# Saved runtime state accepts only documented systemd ActiveState values.
# Kayıtlı çalışma durumu yalnız belgelenmiş systemd ActiveState değerlerini kabul eder.
validate_service_active_state() {
    local unit=$1 state=$2
    case "$state" in
        active|activating|reloading|refreshing|inactive|failed|deactivating|maintenance) ;;
        *) die "unsupported saved active state for $unit: $state" ;;
    esac
}

# Active-like states are restored by starting the service; every other valid
# state is inactive-like and must remain stopped.
# Active-benzeri durumlar servis başlatılarak geri yüklenir; diğer tüm geçerli
# durumlar inactive-benzeridir ve kapalı kalmalıdır.
service_state_is_active_like() {
    case "$1" in
        active|activating|reloading|refreshing) return 0 ;;
        *) return 1 ;;
    esac
}

# Compare one restored service with the active-like/inactive-like contract.
# Geri yüklenen tek servisi active-benzeri/inactive-benzeri sözleşmeyle karşılaştır.
verify_restored_service_active_state() {
    local unit=$1 state=$2
    if service_state_is_active_like "$state"; then
        systemctl is-active --quiet "$unit" \
            || die "restored active-like service is not active: $unit"
    elif systemctl is-active --quiet "$unit"; then
        die "restored inactive-like service became active: $unit"
    fi
}

# Hold the privileged mutation flock through restore. A saved-active agent must
# briefly receive it for startup reconciliation before rollback reacquires it.
# Son boşta doğrulamasından geri yüklenen servisler kayıtlı durumuna ulaşana
# kadar ayrıcalıklı işlem flock kilidini tut.
acquire_release_mutation_lock() {
    local acquire_mode=${1:-immediate}
    local lock_dir group_id owner group mode links size path_identity fd_identity
    [[ $# -le 1 ]] || die "release mutation lock accepts at most one acquisition mode"
    case "$acquire_mode" in
        immediate | handoff) ;;
        *) die "unsupported release mutation lock acquisition mode: $acquire_mode" ;;
    esac
    command -v flock >/dev/null || die "flock is required for a safe rollback"
    group_id=$(getent group celikpanel | cut -d: -f3) \
        || die "celikpanel group is unavailable for the mutation lock"
    [[ "$group_id" =~ ^[0-9]+$ ]] || die "celikpanel group id is invalid"
    lock_dir=$(dirname "$MUTATION_LOCK")
    validate_root_trusted_dir_chain "$lock_dir"
    [[ -d "$lock_dir" && ! -L "$lock_dir" ]] || die "unsafe mutation lock directory: $lock_dir"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
    if [[ -e "$MUTATION_LOCK" || -L "$MUTATION_LOCK" ]]; then
        [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] || die "unsafe mutation lock file"
    else
        [[ "$acquire_mode" == immediate ]] \
            || die "mutation lock pathname disappeared before controlled agent-start handoff reacquire"
        (umask 077; set -o noclobber; : > "$MUTATION_LOCK") \
            || die "cannot exclusively create mutation lock file"
        chown root:celikpanel -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock ownership"
        chmod 0600 -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock permissions"
    fi
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$MUTATION_LOCK") \
        || die "cannot inspect mutation lock file"
    [[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 600 \
        && "$links" == 1 && "$size" == 0 ]] \
        || die "mutation lock file must be root:celikpanel mode 0600, single-link, and empty"
    path_identity=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") \
        || die "cannot identify mutation lock file"
    if [[ "$acquire_mode" == handoff ]]; then
        [[ -n "$MUTATION_LOCK_IDENTITY" ]] \
            || die "mutation lock identity is unavailable for controlled agent-start handoff"
        [[ "$path_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
            || die "mutation lock pathname changed during controlled agent-start handoff"
    else
        MUTATION_LOCK_IDENTITY=$path_identity
    fi
    exec {MUTATION_LOCK_FD}<>"$MUTATION_LOCK"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify opened mutation lock"
    [[ "$fd_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
        || die "mutation lock changed while it was opened"
    if ! flock -n -x "$MUTATION_LOCK_FD"; then
        if [[ "$acquire_mode" == handoff ]]; then
            die "mutation lock was not handed back after controlled agent start; rollback refused"
        fi
        die "a service/package mutation is active; rollback refused"
    fi
}
# Close the exact flock descriptor before rebuilding the RuntimeDirectory lock
# path after agent shutdown.
# Agent kapandıktan sonra RuntimeDirectory lock yolunu yeniden kurmadan önce tam
# flock descriptor'ını kapat.
release_release_mutation_lock() {
    [[ -n "${MUTATION_LOCK_FD:-}" ]] || return 0
    flock -u "$MUTATION_LOCK_FD" || return 1
    exec {MUTATION_LOCK_FD}>&-
    MUTATION_LOCK_FD=
}

verify_restored_agent_idle_under_release_lock() {
    [[ -n "${MUTATION_LOCK_FD:-}" ]] \
        || die "restored agent idle proof requires the release mutation lock"
    case "$transition_state" in
        normal)
            CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
                CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
                "$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock \
                || die "restored agent ledger changed during controlled starts"
            ;;
        pre-ledger|schema17)
            CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
                CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
                "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock \
                || die "restored pre-ledger agent state changed during controlled starts"
            ;;
        *) die "unsupported restored agent transition state: $transition_state" ;;
    esac
}

# A crashed first initialization may leave one canonical temporary stage. The
# trusted checker accepts only that exact pre-ledger shape while the agent is
# stopped. After acquiring the common flock, re-prove the sole stage metadata
# and bytes, unlink only that exact name, and durably sync the directory.
# Çöken ilk başlatma tek bir kanonik geçici stage bırakabilir. Güvenilir checker,
# agent durmuşken yalnız bu tam ledger-öncesi şekli kabul eder. Ortak flock
# alındıktan sonra tek stage metadata ve baytlarını yeniden kanıtla, yalnız bu
# tam adı kaldır ve dizini dayanıklı biçimde eşitle.
cleanup_verified_legacy_initial_stage() {
    local group_id owner group mode links stage_size directory_before directory_after stage stage_name
    local expected_initial_ledger='{"version":1,"jobs":{}}'
    local dotglob_was_set=0 nullglob_was_set=0
    local -a entries=()

    if [[ ! -e "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]]; then
        return 0
    fi
    [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]] \
        || die "legacy private agent state path is unsafe"
    group_id=$(getent group celikpanel | cut -d: -f3) \
        || die "celikpanel group is unavailable"
    [[ "$group_id" =~ ^[0-9]+$ ]] || die "celikpanel group id is invalid"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$AGENT_STATE_DIR") \
        || die "cannot inspect legacy private agent state directory"
    [[ "$owner" == 0 && "$mode" == 700 && ( "$group" == "$group_id" || "$group" == 0 ) ]] \
        || die "legacy private agent state directory must be recoverable root-owned mode 0700"
    directory_before=$(stat -Lc '%d:%i' -- "$AGENT_STATE_DIR") \
        || die "cannot identify legacy private agent state directory"

    shopt -q dotglob && dotglob_was_set=1
    shopt -q nullglob && nullglob_was_set=1
    shopt -s dotglob nullglob
    entries=("$AGENT_STATE_DIR"/*)
    (( dotglob_was_set == 1 )) || shopt -u dotglob
    (( nullglob_was_set == 1 )) || shopt -u nullglob

    if [[ "$group" == 0 ]]; then
        [[ ${#entries[@]} -eq 0 ]] \
            || die "root:root initializer directory residue must be empty"
        return 0
    fi
    [[ ${#entries[@]} -le 1 ]] \
        || die "multiple private agent state entries prevent exact pre-ledger rollback"
    [[ ${#entries[@]} -eq 1 ]] || return 0
    stage=${entries[0]}
    stage_name=${stage##*/}
    [[ "$stage_name" =~ ^\.service-mutations-initial-[0-9]+\.json$ ]] \
        || die "unexpected private agent state prevents exact pre-ledger rollback: $stage"
    [[ -f "$stage" && ! -L "$stage" ]] \
        || die "legacy initializer stage is not a regular file"
    read -r owner group mode links stage_size < <(stat -Lc '%u %g %a %h %s' -- "$stage") \
        || die "cannot inspect legacy initializer stage"
    [[ "$owner" == 0 && ( "$group" == "$group_id" || "$group" == 0 ) && \
       "$mode" == 600 && "$links" == 1 && "$stage_size" =~ ^[0-9]+$ ]] \
        || die "legacy initializer stage metadata is unsafe"
    (( stage_size <= ${#expected_initial_ledger} )) \
        || die "legacy initializer stage exceeds the canonical initial ledger"
    printf '%s' "${expected_initial_ledger:0:stage_size}" | cmp -s - "$stage" \
        || die "legacy initializer stage is not a canonical bounded prefix"
    command -v sync >/dev/null 2>&1 \
        || die "sync is required to durably remove a legacy initializer stage"
    rm -f -- "$stage" || die "cannot remove verified legacy initializer stage"
    [[ ! -e "$stage" && ! -L "$stage" ]] \
        || die "verified legacy initializer stage still exists after removal"
    directory_after=$(stat -Lc '%d:%i' -- "$AGENT_STATE_DIR") \
        || die "cannot re-identify legacy private agent state directory"
    [[ "$directory_before" == "$directory_after" ]] \
        || die "legacy private agent state directory changed during stage cleanup"
    sync -d -- "$AGENT_STATE_DIR" \
        || die "cannot durably sync legacy private agent state cleanup"

    shopt -q dotglob && dotglob_was_set=1 || dotglob_was_set=0
    shopt -q nullglob && nullglob_was_set=1 || nullglob_was_set=0
    shopt -s dotglob nullglob
    entries=("$AGENT_STATE_DIR"/*)
    (( dotglob_was_set == 1 )) || shopt -u dotglob
    (( nullglob_was_set == 1 )) || shopt -u nullglob
    [[ ${#entries[@]} -eq 0 ]] \
        || die "legacy private agent state is not empty after verified stage cleanup"
}
# Prove one trusted agent-ledger policy, close its proof-to-stop race with the
# common flock, stop the whole agent cgroup, then repeat the proof against the
# recreated runtime lock inode and retain that exact flock through restore.
# Güvenilir tek agent-ledger politikasını kanıtla, ortak flock ile kanıt-durdurma
# yarışını kapat, tüm agent cgroup'unu durdur, sonra yeniden oluşturulan runtime
# lock inode'unda kanıtı tekrarla ve geri yükleme boyunca o tam flock'u tut.
stop_new_agent_and_hold_mutation_lock() {
    local checker=$1 held_checker=$2 initial_error=$3 stopped_error=$4
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" "$checker" \
        || die "$initial_error"
    acquire_release_mutation_lock
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$PREFLIGHT_AGENT" "$held_checker" \
        || die "agent/package state changed before the locked stop; rollback refused"
    systemctl stop celikpanel-agent.service || die "agent could not be stopped"
    if systemctl is-active --quiet celikpanel-agent.service; then
        die "agent is still active; rollback refused"
    fi
    reject_extra_service_cgroup_processes celikpanel-agent.service 0
    release_release_mutation_lock \
        || die "cannot release stale mutation lock after agent stop"
    prepare_runtime_mutation_lock_dir
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" "$checker" \
        || die "$stopped_error"
    acquire_release_mutation_lock
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$PREFLIGHT_AGENT" "$held_checker" \
        || die "agent/package state changed before the locked restore; rollback refused"
}
# Restore availability only while no installed byte has changed. Once rollback
# mutation starts, fail closed with both services stopped and an exact retry.
# Kurulu hiçbir bayt değişmemişken yalnız erişilebilirliği geri getir. Geri alma
# mutasyonu başladıktan sonra iki servisi kapalı bırakıp tam yeniden deneme ver.
rollback_on_exit() {
    local status=$? frozen_cleanup_failed=0
    trap - EXIT

    if [[ $legacy_agent_frozen -eq 1 ]]; then
        if [[ $rollback_transaction_started -eq 0 &&
              $rollback_mutation_started -eq 0 &&
              $rollback_service_state_recorded -eq 1 ]]; then
            if ! unfreeze_legacy_agent; then
                terminate_frozen_legacy_agent_fail_closed || frozen_cleanup_failed=1
            fi
        else
            terminate_frozen_legacy_agent_fail_closed || frozen_cleanup_failed=1
        fi
    fi
    if [[ $frozen_cleanup_failed -eq 1 ]]; then
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop --no-block celikpanel-agent.service >/dev/null 2>&1 || true
        release_release_mutation_lock >/dev/null 2>&1 || true
        echo "!! Frozen pre-ledger agent cleanup was not provably complete; both services remain fail-closed." >&2
        [[ $status -ne 0 ]] && return "$status"
        return 1
    fi
    if [[ $status -eq 0 ]]; then
        return 0
    fi
    if [[ $rollback_completion_verified -eq 1 &&
          $rollback_completion_removing -eq 1 &&
          $rollback_scheduler_restore_pending -eq 1 &&
          $rollback_transaction_started -eq 1 &&
          $rollback_mutation_started -eq 1 &&
          ! -e "$RELEASE_TRANSACTION_ROOT/completion.pending" &&
          ! -L "$RELEASE_TRANSACTION_ROOT/completion.pending" &&
          ( -e "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ||
            -L "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ) ]] &&
       release_txn_validate_scheduler_restore_token \
           "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" \
           rollback "$(basename -- "$rollback_verified_snapshot")"; then
        if ! release_release_mutation_lock >/dev/null 2>&1; then
            echo "!! Runtime completion is visible and exact, but the mutation lock could not be released cleanly." >&2
        fi
        echo "!! Rollback runtime completion is visible; completion marker removal durability is uncertain. Restored runtime was left intact and exact scheduler recovery remains retryable." >&2
        echo "!! Verified snapshot / Doğrulanmış snapshot: $rollback_verified_snapshot" >&2
        echo "!! Retry / Yeniden deneyin: sudo /bin/bash '$TRUSTED_RELEASE_ROOT/rollback.sh' '$rollback_verified_snapshot'" >&2
        return "$status"
    fi
    if [[ $rollback_scheduler_restore_pending -eq 1 &&
          $rollback_transaction_started -eq 0 ]]; then
        if [[ $rollback_scheduler_restore_completed -eq 1 &&
              ! -e "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" &&
              ! -L "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ]]; then
            release_release_mutation_lock >/dev/null 2>&1 || true
            echo "!! Certbot scheduler restoration completed; durable marker removal is uncertain. Runtime was left intact and rollback did not claim success." >&2
            echo "!! Verified snapshot / DoÄŸrulanmÄ±ÅŸ snapshot: $rollback_verified_snapshot" >&2
            return "$status"
        fi
        if [[ -e "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ||
              -L "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ]] &&
           release_txn_validate_scheduler_restore_token \
               "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" \
               rollback "$(basename -- "$rollback_verified_snapshot")"; then
            if ! release_release_mutation_lock >/dev/null 2>&1; then
                echo "!! Scheduler recovery remains pending and the mutation lock could not be released cleanly." >&2
            fi
            echo "!! Rollback runtime is complete; exact Certbot scheduler restoration remains safely retryable." >&2
            echo "!! Verified snapshot / Doğrulanmış snapshot: $rollback_verified_snapshot" >&2
            echo "!! Retry / Yeniden deneyin: sudo /bin/bash '$TRUSTED_RELEASE_ROOT/rollback.sh' '$rollback_verified_snapshot'" >&2
            return "$status"
        fi
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
        release_release_mutation_lock >/dev/null 2>&1 || true
        echo "!! Scheduler restoration failed without an exact durable retry marker; both services were stopped." >&2
        return "$status"
    fi
    if [[ $rollback_transaction_started -eq 1 ]]; then
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
        echo "!! Rollback transaction remains pending; both services were left stopped for exact recovery." >&2
        echo "!! Geri alma işlemi beklemede kaldı; tam kurtarma için iki servis kapalı bırakıldı." >&2
        echo "!! Verified snapshot / Doğrulanmış snapshot: $rollback_verified_snapshot" >&2
        echo "!! Retry / Yeniden deneyin: sudo /bin/bash '$TRUSTED_RELEASE_ROOT/rollback.sh' '$rollback_verified_snapshot'" >&2
        return "$status"
    fi
    if [[ $rollback_mutation_started -eq 1 ]]; then
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
        echo "!! Rollback failed after installed mutation began; both services were left stopped." >&2
        echo "!! Kurulu mutasyon başladıktan sonra geri alma başarısız oldu; iki servis kapalı bırakıldı." >&2
        echo "!! Verified snapshot / Doğrulanmış snapshot: $rollback_verified_snapshot" >&2
        echo "!! Retry / Yeniden deneyin: sudo /bin/bash '$TRUSTED_RELEASE_ROOT/rollback.sh' '$rollback_verified_snapshot'" >&2
        return "$status"
    fi
    if [[ $rollback_service_state_recorded -eq 1 ]]; then
        if [[ $rollback_agent_was_active -eq 1 ]] && ! systemctl is-active --quiet celikpanel-agent.service; then
            systemctl start celikpanel-agent.service >/dev/null 2>&1 || true
        fi
        if [[ $rollback_panel_was_active -eq 1 ]] && ! systemctl is-active --quiet celikpanel-panel.service; then
            systemctl start celikpanel-panel.service >/dev/null 2>&1 || true
        fi
        echo "!! Rollback stopped before installed bytes changed; prior active services were restored." >&2
        echo "!! Kurulu baytlar değişmeden geri alma durdu; önceki aktif servisler geri getirildi." >&2
    fi
    return "$status"
}

validate_running_release
# shellcheck source=deploy/release-transaction-guard.sh
source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"
# shellcheck source=deploy/panel-tls-snapshot.sh
source "$TRUSTED_RELEASE_ROOT/deploy/panel-tls-snapshot.sh"
release_txn_verify_inherited_lock "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" || die "persistent release transaction lock verification failed"
install_release_transaction_guards_with_label_barrier \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$RELEASE_TRANSACTION_HELPER" "$RELEASE_TRANSACTION_FD" \
    || die "release transaction service guards could not be installed"
release_txn_clear_stale_start_authorization "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" || die "stale release start authorization could not be cleared"
rollback_quiesce_present=0
rollback_active_present=0
rollback_completion_present=0
rollback_scheduler_present=0
[[ -e "$RELEASE_TRANSACTION_ROOT/quiesce.pending" ||
   -L "$RELEASE_TRANSACTION_ROOT/quiesce.pending" ]] && rollback_quiesce_present=1
[[ -e "$RELEASE_TRANSACTION_ROOT/active" ||
   -L "$RELEASE_TRANSACTION_ROOT/active" ]] && rollback_active_present=1
[[ -e "$RELEASE_TRANSACTION_ROOT/completion.pending" ||
   -L "$RELEASE_TRANSACTION_ROOT/completion.pending" ]] && rollback_completion_present=1
[[ -e "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ||
   -L "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending" ]] && rollback_scheduler_present=1
[[ "$rollback_quiesce_present" -eq 0 ]] \
    || die "quiesce.pending must be recovered by update.sh before rollback"
[[ $((rollback_active_present + rollback_completion_present)) -le 1 ]] \
    || die "ambiguous rollback transaction topology"
[[ ! ( "$rollback_scheduler_present" -eq 1 &&
        "$rollback_active_present" -eq 1 ) ]] \
    || die "scheduler restoration cannot coexist with an active rollback phase"

if [[ "$rollback_completion_present" -eq 1 ]]; then
    IFS=$'\t' read -r pending_token pending_operation pending_snapshot \
        < <(release_txn_read_pending_fields "$RELEASE_TRANSACTION_ROOT") \
        || die "cannot read pending release transaction"
    [[ "$pending_operation" == rollback ]] \
        || die "a pending update must be finalized by update.sh"
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
        || die "pending rollback transaction marker proof failed"
    if [[ "$rollback_scheduler_present" -eq 1 ]]; then
        release_txn_validate_scheduler_restore_token \
            "$RELEASE_TRANSACTION_ROOT" "$pending_token" rollback "$pending_snapshot" \
            || die "pending rollback scheduler marker does not match completion.pending"
    fi
    rollback_pending_resume=1
    rollback_pending_snapshot=$pending_snapshot
    rollback_transaction_token=$pending_token
elif [[ "$rollback_scheduler_present" -eq 1 ]]; then
    IFS=$'\t' read -r scheduler_token scheduler_operation scheduler_snapshot \
        < <(release_txn_read_scheduler_restore_fields "$RELEASE_TRANSACTION_ROOT") \
        || die "cannot read pending scheduler restoration"
    [[ "$scheduler_operation" == rollback ]] \
        || die "a pending update scheduler restoration must be recovered by update.sh"
    release_txn_validate_scheduler_restore_token \
        "$RELEASE_TRANSACTION_ROOT" "$scheduler_token" rollback "$scheduler_snapshot" \
        || die "pending rollback scheduler marker proof failed"
    rollback_scheduler_only_resume=1
    rollback_pending_snapshot=$scheduler_snapshot
    rollback_transaction_token=$scheduler_token
fi
PREFLIGHT_PANEL="$TRUSTED_RELEASE_ROOT/bin/panel"
PREFLIGHT_AGENT="$TRUSTED_RELEASE_ROOT/bin/agent"
trap rollback_on_exit EXIT

validate_root_trusted_dir_chain "$SNAP_ROOT"
requested=${1:-}
if [[ $rollback_pending_resume -eq 1 ||
      $rollback_scheduler_only_resume -eq 1 ]]; then
    if [[ -n "$requested" ]]; then
        requested=${requested%/}
        case "$requested" in
            "$SNAP_ROOT/$rollback_pending_snapshot"|"$rollback_pending_snapshot") ;;
            *) die "pending rollback is bound to snapshot $rollback_pending_snapshot" ;;
        esac
    fi
    requested=$rollback_pending_snapshot
fi
if [[ -z "$requested" ]]; then
    requested=$(find "$SNAP_ROOT" -mindepth 1 -maxdepth 1 -type d ! -name '.*' -printf '%T@ %f\n' \
        | LC_ALL=C sort -nr | awk 'NR == 1 { print $2 }')
fi
[[ -n "$requested" ]] || die "no update snapshot found under $SNAP_ROOT"
requested=${requested%/}
case "$requested" in
    "$SNAP_ROOT"/*) snapshot_name=${requested#"$SNAP_ROOT/"} ;;
    */*) die "snapshot path is outside $SNAP_ROOT" ;;
    *) snapshot_name=$requested ;;
esac
snapshot_name_pattern='^([0-9]{8}T[0-9]{6}Z)-from-unknown-to-([0-9a-f]{40})-([0-9a-f]{32})$'
[[ "$snapshot_name" =~ $snapshot_name_pattern ]] \
    || die "invalid snapshot name: $snapshot_name"
snapshot_name_created_at=${BASH_REMATCH[1]}
snapshot_name_target_commit=${BASH_REMATCH[2]}
snapshot_nonce=${BASH_REMATCH[3]}
snap="$SNAP_ROOT/$snapshot_name"
[[ -d "$snap" && ! -L "$snap" ]] || die "snapshot does not exist or is unsafe: $snap"
validate_root_trusted_dir_chain "$snap"

# Snapshot payloads must be plain directories and regular files. Symlinks would
# make checksum verification and privileged restore target different objects.
# Snapshot ürünleri düz dizin ve normal dosya olmalıdır. Sembolik bağlantılar,
# checksum ile root yetkili geri yüklemenin farklı nesneleri görmesine yol açar.
if find "$snap" -type l -print -quit | grep -q .; then
    die "snapshot contains a symbolic link / snapshot sembolik bağlantı içeriyor"
fi
if find "$snap" ! -type d ! -type f -print -quit | grep -q .; then
    die "snapshot contains a special filesystem object"
fi

[[ -f "$snap/SHA256SUMS" && ! -L "$snap/SHA256SUMS" ]] \
    || die "SHA256SUMS is missing or unsafe"
read -r manifest_owner manifest_group manifest_mode manifest_links manifest_size \
    < <(stat -Lc '%u %g %a %h %s' -- "$snap/SHA256SUMS") \
    || die "cannot inspect snapshot checksum manifest"
manifest_permissions=$((8#$manifest_mode))
[[ "$manifest_owner" == 0 && "$manifest_group" == 0 && "$manifest_links" == 1 &&
   "$manifest_size" -gt 0 && "$manifest_size" -le 16777216 ]] &&
    (( (manifest_permissions & 0022) == 0 )) \
    || die "snapshot checksum manifest metadata is unsafe"

outer_manifest_verified=0
# Reject manifest paths that could escape the verified snapshot directory.
# Doğrulanmış snapshot dizininin dışına çıkabilecek manifest yollarını reddet.
while IFS= read -r checksum_line; do
    manifest_path=${checksum_line#*  }
    [[ "$manifest_path" == ./* ]] || die "unsafe checksum path: $manifest_path"
    [[ "$manifest_path" != *'/../'* && "$manifest_path" != '../'* ]] || die "unsafe checksum traversal: $manifest_path"
done < "$snap/SHA256SUMS"
(
    cd "$snap"
    LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 sha256sum \
        | cmp -s - SHA256SUMS
    sha256sum -c SHA256SUMS >/dev/null
) || die "snapshot checksum verification failed / snapshot checksum doğrulaması başarısız"
outer_manifest_verified=1
[[ "$outer_manifest_verified" -eq 1 ]] \
    || die "outer snapshot manifest verification barrier was not reached"

[[ -f "$snap/snapshot.version" ]] || die "snapshot.version is missing"
version=$(tr -d '[:space:]' < "$snap/snapshot.version")
case "$version" in
    "$SUPPORTED_SNAPSHOT_VERSION") ;;
    5)
        die "snapshot version 5 predates exact installed updater rollback state; use its matching historical recovery release or create a fresh version 6 snapshot"
        ;;
    4)
        die "snapshot version 4 predates exact panel TLS rollback state; use its matching historical recovery release or create a fresh version 6 snapshot"
        ;;
    *) die "unsupported snapshot version: $version" ;;
esac
[[ -f "$snap/commit" ]] || die "commit provenance is missing"
[[ -f "$snap/target-release.commit" ]] || die "target release commit is missing"
[[ -f "$snap/target-release.tree" ]] || die "target release tree is missing"
[[ -f "$snap/created-at-utc" ]] || die "creation time is missing"
[[ -f "$snap/$(basename "$PANEL_DB")" ]] || die "panel database is missing from snapshot"
[[ ! -e "$snap/$(basename "$PANEL_DB")-wal" && \
   ! -L "$snap/$(basename "$PANEL_DB")-wal" && \
   ! -e "$snap/$(basename "$PANEL_DB")-shm" && \
   ! -L "$snap/$(basename "$PANEL_DB")-shm" && \
   ! -e "$snap/$(basename "$PANEL_DB")-journal" && \
   ! -L "$snap/$(basename "$PANEL_DB")-journal" ]] \
    || die "panel database snapshot must be standalone without WAL/SHM/journal"
[[ -x "$snap/bin/panel" ]] || die "panel binary is missing from snapshot"
[[ -x "$snap/bin/agent" ]] || die "agent binary is missing from snapshot"
[[ -f "$snap/web/index.html" ]] || die "web artifact is missing from snapshot"
[[ -f "$snap/units/celikpanel-agent.service" ]] || die "agent unit is missing from snapshot"
[[ -f "$snap/units/celikpanel-panel.service" ]] || die "panel unit is missing from snapshot"
[[ -f "$snap/firewall-unit.state" ]] || die "firewall unit presence marker is missing"
[[ -f "$snap/agent-ledger.state" ]] || die "agent ledger presence marker is missing"
[[ -f "$snap/agent-state-root" ]] || die "agent state root marker is missing"
[[ -d "$snap/agent-state" ]] || die "agent state payload directory is missing"
[[ -f "$snap/service-states.tsv" ]] || die "service state ledger is missing"
[[ -f "$snap/snapshot-transition.state" ]] || die "snapshot transition state is missing"
[[ -f "$snap/release-updater.state" && ! -L "$snap/release-updater.state" ]] \
    || die "release updater presence marker is missing or unsafe"
release_updater_state=$(tr -d '[:space:]' < "$snap/release-updater.state")
case "$release_updater_state" in
    present)
        [[ -f "$snap/libexec/get.sh" && ! -L "$snap/libexec/get.sh" ]] \
            || die "snapshot release updater is missing or unsafe"
        read -r updater_owner updater_group updater_mode updater_links < <(
            stat -Lc '%u %g %a %h' -- "$snap/libexec/get.sh"
        ) || die "cannot inspect snapshot release updater"
        [[ "$updater_owner:$updater_group:$updater_mode:$updater_links" == 0:0:755:1 ]] \
            || die "snapshot release updater metadata is unsafe"
        ;;
    absent)
        [[ ! -e "$snap/libexec/get.sh" && ! -L "$snap/libexec/get.sh" ]] \
            || die "snapshot marks release updater absent but includes bytes"
        ;;
    *) die "invalid release updater presence marker" ;;
esac

firewall_state=$(tr -d '[:space:]' < "$snap/firewall-unit.state")
case "$firewall_state" in
    present)
        [[ -f "$snap/units/celikpanel-firewall-restore.service" ]] || die "firewall unit is marked present but missing"
        ;;
    absent)
        [[ ! -e "$snap/units/celikpanel-firewall-restore.service" ]] || die "firewall unit is marked absent but included"
        ;;
    *)
        die "invalid firewall unit state: $firewall_state"
        ;;
esac

agent_state_root=$(tr -d '[:space:]' < "$snap/agent-state-root")
[[ "$agent_state_root" == "$AGENT_STATE_DIR" ]] \
    || die "snapshot agent state root is incompatible: $agent_state_root"

agent_ledger_state=$(tr -d '[:space:]' < "$snap/agent-ledger.state")
case "$agent_ledger_state" in
    present)
        [[ -f "$snap/agent-state/service-mutations.json" ]] || die "agent ledger is marked present but missing"
        ;;
    absent)
        [[ ! -e "$snap/agent-state/service-mutations.json" ]] || die "agent ledger is marked absent but included"
        ;;
    *)
        die "invalid agent ledger state: $agent_ledger_state"
        ;;
esac

# The panel TLS helper reads layout, ownership and scheduler metadata. Treat it
# as payload interpretation: never inspect those values until the complete
# outer manifest has proved the exact file set and bytes.
panel_tls_snapshot_validate "$snap/panel-tls" \
    || die "panel TLS compatibility snapshot is missing or invalid"

# Interpret transition metadata only after the exact outer manifest has been
# verified. Normal v6 snapshots must not carry bootstrap-only payloads.
# Geçiş metadata'sını yalnız tam dış manifest doğrulandıktan sonra yorumla.
# Normal v6 snapshotlar yalnız bootstrap'a ait ürünleri taşımamalıdır.
snapshot_commit=$(cat "$snap/commit")
snapshot_created_at=$(cat "$snap/created-at-utc")
target_release_commit=$(cat "$snap/target-release.commit")
target_release_tree=$(cat "$snap/target-release.tree")
[[ "$snapshot_commit" == unknown ]] || die "snapshot source commit must be unknown unless independently attested"
[[ "$snapshot_created_at" =~ ^[0-9]{8}T[0-9]{6}Z$ ]] || die "invalid snapshot creation time"
[[ "$target_release_commit" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid target release commit"
[[ "$target_release_tree" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid target release tree"
[[ "$target_release_commit" == "$trusted_rollback_release_commit" ]] \
    || die "snapshot target commit does not match the running trusted rollback release"
[[ "$target_release_tree" == "$trusted_rollback_release_tree" ]] \
    || die "snapshot target tree does not match the running trusted rollback release"
[[ "$snapshot_name_created_at" == "$snapshot_created_at" ]] \
    || die "snapshot name does not match creation provenance"
[[ "$snapshot_name_target_commit" == "$target_release_commit" ]] \
    || die "snapshot name does not match target release provenance"
transition_state=$(cat "$snap/snapshot-transition.state")
case "$transition_state" in
    normal)
        printf 'normal\n' | cmp -s - "$snap/snapshot-transition.state" \
            || die "normal transition state marker is not exact"
        [[ "$agent_ledger_state" == present ]] \
            || die "normal v6 snapshot must contain the durable agent ledger"
        [[ ! -e "$snap/pre-ledger-transition.tsv" && \
           ! -e "$snap/pre-ledger-transition.sha256" && \
           ! -e "$snap/schema17-transition.tsv" && \
           ! -e "$snap/schema17-transition.sha256" && \
           ! -e "$snap/transition-preflight" ]] \
            || die "normal v6 snapshot contains bootstrap transition payloads"
        ;;
    pre-ledger)
        printf 'pre-ledger\n' | cmp -s - "$snap/snapshot-transition.state" \
            || die "pre-ledger transition state marker is not exact"
        [[ "$agent_ledger_state" == absent ]] \
            || die "pre-ledger v6 snapshot must not contain the durable agent ledger"
        [[ ! -e "$snap/schema17-transition.tsv" && \
           ! -e "$snap/schema17-transition.sha256" ]] \
            || die "pre-ledger v6 snapshot contains schema17 transition payloads"
        [[ -f "$snap/pre-ledger-transition.tsv" && ! -L "$snap/pre-ledger-transition.tsv" ]] \
            || die "pre-ledger transition marker is missing or unsafe"
        [[ -f "$snap/pre-ledger-transition.sha256" && ! -L "$snap/pre-ledger-transition.sha256" ]] \
            || die "pre-ledger transition checksum is missing or unsafe"
        [[ -d "$snap/transition-preflight" && ! -L "$snap/transition-preflight" ]] \
            || die "pre-ledger transition preflight directory is missing or unsafe"
        [[ -x "$snap/transition-preflight/panel" && -x "$snap/transition-preflight/agent" ]] \
            || die "pre-ledger transition checker binaries are missing"
        [[ $(find "$snap/transition-preflight" -mindepth 1 -maxdepth 1 | wc -l) -eq 2 ]] \
            || die "pre-ledger transition preflight payload is not exact"
        marker_checksum=$(cat "$snap/pre-ledger-transition.sha256")
        marker_pattern='^([0-9a-f]{64})  pre-ledger-transition\.tsv$'
        [[ "$marker_checksum" =~ $marker_pattern ]] \
            || die "pre-ledger transition checksum manifest is malformed"
        (
            cd "$snap"
            sha256sum -c pre-ledger-transition.sha256 >/dev/null
        ) || die "pre-ledger transition marker checksum failed"

        declare -A transition_values=()
        while IFS=$'\t' read -r transition_key transition_value transition_extra; do
            [[ -n "$transition_key" && -n "$transition_value" && -z "${transition_extra:-}" ]] \
                || die "malformed pre-ledger transition marker"
            case "$transition_key" in
                transition-version|mode|source-schema-version|target-release-commit|target-release-tree|agent-state-root|created-at-utc) ;;
                *) die "unknown pre-ledger transition key: $transition_key" ;;
            esac
            [[ -z "${transition_values[$transition_key]+x}" ]] \
                || die "duplicate pre-ledger transition key: $transition_key"
            transition_values["$transition_key"]=$transition_value
        done < "$snap/pre-ledger-transition.tsv"
        for transition_key in \
            transition-version mode source-schema-version target-release-commit \
            target-release-tree agent-state-root created-at-utc; do
            [[ -n "${transition_values[$transition_key]:-}" ]] \
                || die "missing pre-ledger transition key: $transition_key"
        done
        [[ "${#transition_values[@]}" -eq 7 ]] \
            || die "pre-ledger transition marker has an unexpected key count"
        [[ "${transition_values[transition-version]}" == 1 ]] \
            || die "unsupported pre-ledger transition version"
        [[ "${transition_values[mode]}" == bootstrap-pre-ledger ]] \
            || die "invalid pre-ledger transition mode"
        [[ "${transition_values[source-schema-version]}" == 20 ]] \
            || die "invalid pre-ledger source schema version"
        [[ "${transition_values[target-release-commit]}" =~ ^[0-9a-f]{40,64}$ ]] \
            || die "invalid pre-ledger release commit"
        [[ "${transition_values[target-release-tree]}" =~ ^[0-9a-f]{40,64}$ ]] \
            || die "invalid pre-ledger release tree"
        [[ "${transition_values[agent-state-root]}" == "$AGENT_STATE_DIR" ]] \
            || die "invalid pre-ledger agent state root"
        [[ "${transition_values[created-at-utc]}" =~ ^[0-9]{8}T[0-9]{6}Z$ ]] \
            || die "invalid pre-ledger creation time"
        [[ "$target_release_commit" == "${transition_values[target-release-commit]}" ]] \
            || die "pre-ledger release commit does not match target provenance"
        [[ "$target_release_tree" == "${transition_values[target-release-tree]}" ]] \
            || die "pre-ledger release tree does not match target provenance"
        [[ "$snapshot_created_at" == "${transition_values[created-at-utc]}" ]] \
            || die "pre-ledger creation time does not match snapshot provenance"


        ;;
    schema17)
        printf 'schema17\n' | cmp -s - "$snap/snapshot-transition.state" \
            || die "schema17 transition state marker is not exact"
        [[ "$agent_ledger_state" == absent ]] \
            || die "schema17 v6 snapshot must not contain the durable agent ledger"
        [[ ! -e "$snap/pre-ledger-transition.tsv" && \
           ! -e "$snap/pre-ledger-transition.sha256" ]] \
            || die "schema17 v6 snapshot contains pre-ledger transition payloads"
        [[ -f "$snap/schema17-transition.tsv" && ! -L "$snap/schema17-transition.tsv" ]] \
            || die "schema17 transition marker is missing or unsafe"
        [[ -f "$snap/schema17-transition.sha256" && ! -L "$snap/schema17-transition.sha256" ]] \
            || die "schema17 transition checksum is missing or unsafe"
        [[ -d "$snap/transition-preflight" && ! -L "$snap/transition-preflight" ]] \
            || die "schema17 transition preflight directory is missing or unsafe"
        [[ -x "$snap/transition-preflight/panel" && \
           -x "$snap/transition-preflight/agent" && \
           -x "$snap/transition-preflight/schema17-bridge" ]] \
            || die "schema17 transition checker binaries are missing"
        [[ $(find "$snap/transition-preflight" -mindepth 1 -maxdepth 1 | wc -l) -eq 3 ]] \
            || die "schema17 transition preflight payload is not exact"
        marker_checksum=$(cat "$snap/schema17-transition.sha256")
        marker_pattern='^([0-9a-f]{64})  schema17-transition\.tsv$'
        [[ "$marker_checksum" =~ $marker_pattern ]] \
            || die "schema17 transition checksum manifest is malformed"
        (
            cd "$snap"
            sha256sum -c schema17-transition.sha256 >/dev/null
        ) || die "schema17 transition marker checksum failed"

        declare -A schema17_values=()
        while IFS=$'\t' read -r transition_key transition_value transition_extra; do
            [[ -n "$transition_key" && -n "$transition_value" && -z "${transition_extra:-}" ]] \
                || die "malformed schema17 transition marker"
            case "$transition_key" in
                transition-version|mode|source-schema-version|bridge-schema-version|target-release-commit|target-release-tree|agent-state-root|created-at-utc) ;;
                *) die "unknown schema17 transition key: $transition_key" ;;
            esac
            [[ -z "${schema17_values[$transition_key]+x}" ]] \
                || die "duplicate schema17 transition key: $transition_key"
            schema17_values["$transition_key"]=$transition_value
        done < "$snap/schema17-transition.tsv"
        for transition_key in \
            transition-version mode source-schema-version bridge-schema-version \
            target-release-commit target-release-tree agent-state-root created-at-utc; do
            [[ -n "${schema17_values[$transition_key]:-}" ]] \
                || die "missing schema17 transition key: $transition_key"
        done
        [[ "${#schema17_values[@]}" -eq 8 ]] \
            || die "schema17 transition marker has an unexpected key count"
        [[ "${schema17_values[transition-version]}" == 1 ]] \
            || die "unsupported schema17 transition version"
        [[ "${schema17_values[mode]}" == bootstrap-schema17 ]] \
            || die "invalid schema17 transition mode"
        [[ "${schema17_values[source-schema-version]}" == 17 ]] \
            || die "invalid schema17 source schema version"
        [[ "${schema17_values[bridge-schema-version]}" == 20 ]] \
            || die "invalid schema17 bridge schema version"
        [[ "${schema17_values[target-release-commit]}" =~ ^[0-9a-f]{40,64}$ ]] \
            || die "invalid schema17 release commit"
        [[ "${schema17_values[target-release-tree]}" =~ ^[0-9a-f]{40,64}$ ]] \
            || die "invalid schema17 release tree"
        [[ "${schema17_values[agent-state-root]}" == "$AGENT_STATE_DIR" ]] \
            || die "invalid schema17 agent state root"
        [[ "${schema17_values[created-at-utc]}" =~ ^[0-9]{8}T[0-9]{6}Z$ ]] \
            || die "invalid schema17 creation time"
        [[ "$target_release_commit" == "${schema17_values[target-release-commit]}" ]] \
            || die "schema17 release commit does not match target provenance"
        [[ "$target_release_tree" == "${schema17_values[target-release-tree]}" ]] \
            || die "schema17 release tree does not match target provenance"
        [[ "$snapshot_created_at" == "${schema17_values[created-at-utc]}" ]] \
            || die "schema17 creation time does not match snapshot provenance"
        PREFLIGHT_SCHEMA17_BRIDGE="$snap/transition-preflight/schema17-bridge"
        ;;
    *) die "invalid snapshot transition state: $transition_state" ;;
esac

# Validate the state ledger before service shutdown. Only the three units owned
# by this release may be changed by rollback.
# Servisleri durdurmadan önce durum defterini doğrula. Geri alma yalnız bu
# sürümün sahip olduğu üç unit'i değiştirebilir.
declare -A enabled_states=()
declare -A active_states=()
service_state_count=0
while IFS=$'\t' read -r unit enabled_state active_state extra ||
      [[ -n "$unit$enabled_state$active_state${extra:-}" ]]; do
    [[ -n "$unit" && -n "$enabled_state" && -n "$active_state" && -z "${extra:-}" ]] || die "malformed service state ledger"
    case "$service_state_count" in
        0) expected_unit=celikpanel-agent.service ;;
        1) expected_unit=celikpanel-panel.service ;;
        2) expected_unit=celikpanel-firewall-restore.service ;;
        3) expected_unit=certbot.timer ;;
        4) expected_unit=certbot-renew.timer ;;
        *) die "service state ledger contains extra rows" ;;
    esac
    [[ "$unit" == "$expected_unit" ]] \
        || die "service state ledger order is not canonical: got $unit, want $expected_unit"
    if [[ "$service_state_count" -lt 3 ]]; then
        case "$enabled_state" in
            enabled|enabled-runtime|disabled|static|indirect|not-found) ;;
            *) die "unsupported saved enable state for $unit: $enabled_state" ;;
        esac
        validate_service_active_state "$unit" "$active_state"
        enabled_states["$unit"]=$enabled_state
        active_states["$unit"]=$active_state
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
    service_state_count=$((service_state_count + 1))
done < "$snap/service-states.tsv"
[[ "$service_state_count" -eq 5 ]] \
    || die "service state ledger must contain exactly five canonical rows"
panel_tls_snapshot_scheduler_matches_service_ledger \
    "$snap/panel-tls" "$snap/service-states.tsv" \
    || die "panel TLS scheduler snapshot disagrees with the service ledger"
for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
    [[ -n "${enabled_states[$unit]:-}" ]] || die "service state is missing for $unit"
done
case "$firewall_state:${enabled_states[celikpanel-firewall-restore.service]}" in
    absent:not-found|present:*) ;;
    *) die "firewall unit presence and enablement state disagree" ;;
esac
if service_state_is_active_like "${active_states[celikpanel-panel.service]}" && \
   ! service_state_is_active_like "${active_states[celikpanel-agent.service]}"; then
    die "saved runtime state is inconsistent: an active panel requires an active agent"
fi

validate_preflight_binary "$PREFLIGHT_PANEL" panel
validate_preflight_binary "$PREFLIGHT_AGENT" agent
if [[ "$transition_state" == schema17 ]]; then
    validate_preflight_binary "$PREFLIGHT_SCHEMA17_BRIDGE" schema17-bridge
fi
case "$transition_state" in
    normal)
        CELIKPANEL_DATA_DIR="$snap" \
            "$PREFLIGHT_PANEL" --check-service-operations-idle \
            || die "verified normal snapshot database is not exact and idle"
        ;;
    pre-ledger)
        CELIKPANEL_DATA_DIR="$snap" \
            "$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle \
            || die "verified pre-ledger snapshot database is not exact schema version 20"
        ;;
    schema17)
        "$PREFLIGHT_SCHEMA17_BRIDGE" check \
            --db "$snap/$(basename "$PANEL_DB")" \
            || die "verified schema17 snapshot database is not exact schema version 17"
        ;;
esac

rollback_verified_snapshot=$snap
if [[ $rollback_scheduler_only_resume -eq 1 ]]; then
    release_txn_validate_scheduler_restore_token \
        "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "rollback scheduler marker changed after snapshot verification"
    rollback_scheduler_restore_pending=1
    panel_tls_quiesce_certbot_scheduler "$snap/panel-tls" \
        || die "Certbot renewal scheduler could not be re-quiesced for exact recovery"
    release_txn_validate_scheduler_restore_token \
        "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "rollback scheduler marker changed during exact recovery"
    panel_tls_restore_certbot_scheduler "$snap/panel-tls" \
        || die "Certbot renewal scheduler state could not be restored"
    rollback_scheduler_restore_completed=1
    release_txn_remove_scheduler_restore_pending \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "cannot remove the exact rollback scheduler marker"
    rollback_scheduler_restore_pending=0
    trap - EXIT
    echo
    echo "==> Rollback runtime was already complete; Certbot scheduler restoration is complete."
    exit 0
fi
if [[ $rollback_pending_resume -eq 1 ]]; then
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "pending rollback transaction marker proof failed"
elif [[ -e "$RELEASE_TRANSACTION_ROOT/active" || -L "$RELEASE_TRANSACTION_ROOT/active" ]]; then
    rollback_transaction_token=$(release_txn_takeover_active_for_rollback \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" "$snapshot_name") \
        || die "active release transaction cannot be taken over for this rollback"
else
    rollback_transaction_token=$(release_txn_generate_token) \
        || die "cannot generate rollback transaction token"
    release_txn_create_active_marker \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "cannot publish active rollback transaction marker"
fi
rollback_transaction_started=1
if systemctl is-active --quiet celikpanel-panel.service; then
    rollback_panel_was_active=1
fi
if systemctl is-active --quiet celikpanel-agent.service; then
    rollback_agent_was_active=1
fi
rollback_service_state_recorded=1
echo "==> Verified snapshot / Doğrulanmış snapshot: $snap"
echo "==> Stopping CelikPanel services / CelikPanel servisleri durduruluyor"
systemctl stop celikpanel-panel.service || die "panel could not be stopped"
if systemctl is-active --quiet celikpanel-panel.service; then
    die "panel is still active; rollback refused"
fi

# Current panel database bytes are deliberately not a rollback prerequisite.
# The verified snapshot is the trusted database source; after the panel stops,
# only the agent ledger, package-manager probe and common flock attest that no
# privileged host mutation can race or be silently forgotten.
# Geçerli panel veritabanı baytları bilerek geri alma önkoşulu değildir.
# Güvenilir veritabanı kaynağı doğrulanmış snapshot'tır; panel durduktan sonra
# yalnız agent ledger'ı, paket-yöneticisi kanıtı ve ortak flock ayrıcalıklı host
# mutasyonunun yarışamayacağını veya sessizce unutulamayacağını kanıtlar.
current_transition_phase=normal
if [[ "$transition_state" == pre-ledger || "$transition_state" == schema17 ]]; then
    if [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
        current_transition_phase=pre-ledger-legacy
    else
        current_transition_phase=pre-ledger-agent-initialized
    fi
fi

case "$current_transition_phase" in
    normal)
        stop_new_agent_and_hold_mutation_lock \
            --check-service-mutation-idle \
            --check-service-mutation-idle-under-external-lock \
            "agent/package mutations are not idle; rollback refused" \
            "agent/package mutations changed while stopping; rollback refused"
        # A normal snapshot may be restored only if no terminal service mutation
        # was appended after it. Compare only after the agent cgroup is empty and
        # the recreated common flock is held.
        # Normal snapshot yalnız sonrasında terminal servis mutasyonu eklenmediyse
        # geri yüklenebilir. Karşılaştırmayı agent cgroup boş ve yeniden oluşturulan
        # ortak flock tutuluyorken yap.
        cmp -s "$AGENT_LEDGER" "$snap/agent-state/service-mutations.json" \
            || die "current agent ledger differs from the verified snapshot; rollback would forget host mutations"
        ;;
    pre-ledger-agent-initialized)
        stop_new_agent_and_hold_mutation_lock \
            --check-initial-service-mutation-ledger \
            --check-initial-service-mutation-ledger-under-external-lock \
            "initial agent ledger is not exact, safe and idle; pre-ledger rollback refused" \
            "initial agent ledger changed while stopping; pre-ledger rollback refused"
        ;;
    pre-ledger-legacy)
        freeze_and_stop_legacy_agent
        prepare_runtime_mutation_lock_dir
        [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
            || die "durable agent ledger appeared during legacy stop; rollback refused"
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle \
            || die "pre-ledger agent/package state is not idle; rollback refused"
        acquire_release_mutation_lock
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock \
            || die "pre-ledger agent/package state changed before the locked restore; rollback refused"
        cleanup_verified_legacy_initial_stage
        ;;
    *) die "invalid current transition phase: $current_transition_phase" ;;
esac
# A pre-ledger target had no private agent state directory. Refuse to erase any
# unexpected post-upgrade state; the sole known transition artifact is the
# durable service-mutation ledger.
# Ledger öncesi hedefte özel agent durum dizini yoktu. Beklenmeyen yükseltme
# sonrası durumunu silmeyi reddet; bilinen tek geçiş ürünü kalıcı servis-mutasyon
# ledger'ıdır.
if [[ ( "$transition_state" == pre-ledger || "$transition_state" == schema17 ) && \
      ( -e "$AGENT_STATE_DIR" || -L "$AGENT_STATE_DIR" ) ]]; then
    [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]] \
        || die "current private agent state path is unsafe"
    unexpected_agent_state=$(
        find "$AGENT_STATE_DIR" -mindepth 1 -maxdepth 1 \
            ! -name service-mutations.json \
            ! -name panel-certificate-activation.json -print -quit
    )
    [[ -z "$unexpected_agent_state" ]] \
        || die "unexpected private agent state prevents exact pre-ledger rollback: $unexpected_agent_state"
fi

# From the first installed-byte replacement onward, any failure must leave both
# coordinators stopped and the durable transaction marker available for retry.
# İlk kurulu-bayt değişiminden itibaren her hata iki koordinatörü kapalı bırakmalı
# ve dayanıklı işlem işaretçisini yeniden deneme için korumalıdır.
rollback_mutation_started=1
for unit in celikpanel-agent.service celikpanel-panel.service; do
    stopped_state=$(systemctl show --property=ActiveState --value "$unit") \
        || die "cannot inspect stopped rollback service: $unit"
    [[ "$stopped_state" == inactive || "$stopped_state" == failed ]] \
        || die "rollback requires $unit stopped before restore"
done

# Prevent the package-owned renewal scheduler from racing the exact TLS/hook
# restore. Already-running renewal services are never killed mid-transaction;
# rollback stops safely and can be retried after they finish.
panel_tls_quiesce_certbot_scheduler "$snap/panel-tls" \
    || die "Certbot renewal scheduler could not be quiesced safely"

# This restore is deliberately idempotent and therefore also repairs a
# completion-pending retry. It restores the active certificate layout, the
# legacy/current hook contract, and exact pending activation presence/bytes.
panel_tls_restore_snapshot \
    "$snap/panel-tls" "$PANEL_TLS_DIR" "$PANEL_CERT_PENDING" "$PANEL_CERT_HOOK" \
    || die "panel TLS compatibility state could not be restored"

if [[ $rollback_pending_resume -eq 0 ]]; then
    rm -rf -- "$BIN_DIR"
    cp -a "$snap/bin" "$BIN_DIR"
    rm -rf -- "$WEB_DIR"
    cp -a "$snap/web" "$WEB_DIR"

    validate_root_trusted_dir_chain "$LIBEXEC_DIR"
    if [[ -e "$RELEASE_UPDATER" || -L "$RELEASE_UPDATER" ]]; then
        [[ -f "$RELEASE_UPDATER" && ! -L "$RELEASE_UPDATER" ]] \
            || die "current installed release updater is unsafe"
    fi
    if [[ "$release_updater_state" == present ]]; then
        updater_tmp=$(mktemp "$LIBEXEC_DIR/.get.sh.rollback.XXXXXXXX") \
            || die "cannot stage snapshot release updater"
        if ! cp --no-preserve=mode,ownership,timestamps -- "$snap/libexec/get.sh" "$updater_tmp" ||
           ! chown root:root -- "$updater_tmp" || ! chmod 0755 -- "$updater_tmp" ||
           ! cmp -s -- "$snap/libexec/get.sh" "$updater_tmp" || ! sync -f -- "$updater_tmp" ||
           ! mv -T -- "$updater_tmp" "$RELEASE_UPDATER" || ! sync -f -- "$LIBEXEC_DIR"; then
            [[ ! -e "$updater_tmp" && ! -L "$updater_tmp" ]] || rm -f -- "$updater_tmp"
            die "snapshot release updater could not be restored exactly"
        fi
    else
        rm -f -- "$RELEASE_UPDATER"
        sync -f -- "$LIBEXEC_DIR" || die "release updater removal could not be made durable"
    fi

    # A manifest-verified release helper performs the SQLite restore, including
    # sidecar handling and atomic durable replacement. Shell never copies DB bytes.
    # SQLite geri yüklemesini sidecar yönetimi ve atomik dayanıklı değiştirme dahil
    # manifest ile doğrulanmış sürüm yardımcısı yapar. Shell DB baytlarını kopyalamaz.
    if [[ "$transition_state" == schema17 ]]; then
        release_txn_validate_active_token \
            "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
            || die "active rollback marker changed before exact schema17 restore"
        "$PREFLIGHT_SCHEMA17_BRIDGE" restore \
            --db "$PANEL_DB" \
            --snapshot "$snap/$(basename "$PANEL_DB")" \
            || die "trusted schema17 database restore was not confirmed; it may be committed-but-unconfirmed, retry this exact snapshot"
    else
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$PREFLIGHT_PANEL" \
            --restore-service-operation-snapshot="$snap/$(basename "$PANEL_DB")" \
            --snapshot-schema="$transition_state" \
            --release-transaction-fd="$RELEASE_TRANSACTION_FD" \
            --release-transaction-token="$rollback_transaction_token" \
            --release-transaction-operation=rollback \
            --release-transaction-snapshot="$snapshot_name" \
            || die "trusted database restore was not confirmed; it may be committed-but-unconfirmed, retry this exact snapshot"
    fi

    # Restore the paired durable ledger. A pre-ledger target removes the now-empty
    # private directory so the one-time bootstrap can be retried exactly.
    # Eşlenmiş kalıcı ledger'ı geri yükle. Ledger öncesi hedef, tek seferlik bootstrap
    # tam olarak yeniden denenebilsin diye artık boş olan özel dizini kaldırır.
    rm -f -- "$AGENT_LEDGER"
    if [[ "$agent_ledger_state" == present ]]; then
        install -d -m 0700 -o root -g celikpanel "$AGENT_STATE_DIR"
        install -m 0600 -o root -g celikpanel \
            "$snap/agent-state/service-mutations.json" "$AGENT_LEDGER"
    elif [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]]; then
        rmdir -- "$AGENT_STATE_DIR" \
            || die "private agent state directory is not empty after pre-ledger restore"
    elif [[ -e "$AGENT_STATE_DIR" || -L "$AGENT_STATE_DIR" ]]; then
        die "private agent state path became unsafe during rollback"
    fi

    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        systemctl disable "$unit" >/dev/null 2>&1 || true
        rm -f -- "$UNIT_DIR/$unit"
    done
    rm -f -- "$UNIT_DIR/multi-user.target.wants/celikpanel-firewall-restore.service"
    rm -f -- "$UNIT_DIR/network-pre.target.requires/celikpanel-firewall-restore.service"
    cp -a "$snap/units/." "$UNIT_DIR/"
else
    echo "==> Resuming verified pending rollback / Doğrulanmış bekleyen geri alma sürdürülüyor"
fi

# Snapshot copies deliberately preserve archive metadata. Reassert the host
# filesystem policy before systemd reads units or any service can be enabled.
restore_celikpanel_selinux_labels
if [[ $rollback_pending_resume -eq 0 ]]; then
    systemctl daemon-reload
fi

# Restored unit bytes are guarded before enablement or any controlled start.
# Geri yüklenen unit baytları etkinleştirme veya kontrollü başlangıçtan önce korunur.
install_release_transaction_guards_with_label_barrier \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$UNIT_DIR" "$RELEASE_TRANSACTION_HELPER" "$RELEASE_TRANSACTION_FD" \
    || die "restored release transaction service guards could not be verified"

for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
    case "${enabled_states[$unit]}" in
        enabled)
            [[ -f "$UNIT_DIR/$unit" ]] || die "cannot enable absent restored unit: $unit"
            systemctl enable "$unit" >/dev/null
            ;;
        enabled-runtime)
            [[ -f "$UNIT_DIR/$unit" ]] || die "cannot runtime-enable absent restored unit: $unit"
            systemctl enable --runtime "$unit" >/dev/null
            ;;
        disabled)
            systemctl disable "$unit" >/dev/null 2>&1 || true
            ;;
        static|indirect|not-found) ;;
    esac
    actual_enabled_state=$(systemctl is-enabled "$unit" 2>/dev/null || true)
    [[ "${actual_enabled_state:-unknown}" == "${enabled_states[$unit]}" ]] \
        || die "restored enablement mismatch for $unit: got ${actual_enabled_state:-unknown}, want ${enabled_states[$unit]}"
done

# Prove exact restored bytes and both durable coordinators while all starts are
# still blocked. A pending resume must satisfy the same proof without restoring again.
# Tüm başlangıçlar hâlâ engelliyken tam geri yüklenen baytları ve iki kalıcı
# koordinatörü kanıtla. Pending sürdürme yeniden yüklemeden aynı kanıtı sağlamalıdır.
[[ -f "$BIN_DIR/panel" && ! -L "$BIN_DIR/panel" && -f "$BIN_DIR/agent" && ! -L "$BIN_DIR/agent" ]] \
    || die "restored binaries are missing or unsafe"
cmp -s "$snap/bin/panel" "$BIN_DIR/panel" || die "restored panel bytes differ from snapshot"
cmp -s "$snap/bin/agent" "$BIN_DIR/agent" || die "restored agent bytes differ from snapshot"
case "$release_updater_state" in
    present)
        [[ -f "$RELEASE_UPDATER" && ! -L "$RELEASE_UPDATER" ]] \
            || die "restored release updater is missing or unsafe"
        [[ "$(stat -Lc '%u:%g:%a:%h' -- "$RELEASE_UPDATER")" == 0:0:755:1 ]] \
            || die "restored release updater metadata differs from snapshot policy"
        cmp -s "$snap/libexec/get.sh" "$RELEASE_UPDATER" \
            || die "restored release updater bytes differ from snapshot"
        ;;
    absent)
        [[ ! -e "$RELEASE_UPDATER" && ! -L "$RELEASE_UPDATER" ]] \
            || die "release updater exists although snapshot marks it absent"
        ;;
esac
if find "$BIN_DIR" "$WEB_DIR" -type l -print -quit | grep -q .; then
    die "restored binary/web tree contains a symbolic link"
fi
if find "$BIN_DIR" "$WEB_DIR" ! -type d ! -type f -print -quit | grep -q .; then
    die "restored binary/web tree contains a special filesystem object"
fi
cmp -s \
    <(cd "$snap/web" && LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
    <(cd "$WEB_DIR" && LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
    || die "restored web tree differs from snapshot"
for unit in celikpanel-agent.service celikpanel-panel.service; do
    cmp -s "$snap/units/$unit" "$UNIT_DIR/$unit" \
        || die "restored unit differs from snapshot: $unit"
done
if [[ "$firewall_state" == present ]]; then
    cmp -s "$snap/units/celikpanel-firewall-restore.service" "$UNIT_DIR/celikpanel-firewall-restore.service" \
        || die "restored firewall unit differs from snapshot"
elif [[ -e "$UNIT_DIR/celikpanel-firewall-restore.service" || -L "$UNIT_DIR/celikpanel-firewall-restore.service" ]]; then
    die "firewall unit exists although snapshot marks it absent"
fi

case "$transition_state" in
    normal)
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$PREFLIGHT_PANEL" --check-service-operations-idle \
            || die "restored normal panel database is not exact and idle"
        [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
            || die "restored agent ledger is missing or unsafe"
        cmp -s "$snap/agent-state/service-mutations.json" "$AGENT_LEDGER" \
            || die "restored agent ledger differs from snapshot"
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock \
            || die "restored agent ledger is not idle under the release lock"
        ;;
    pre-ledger)
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle \
            || die "restored pre-ledger panel database is not exact schema version 20"
        [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
            || die "pre-ledger rollback unexpectedly restored an agent ledger"
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock \
            || die "restored pre-ledger agent state is not idle under the release lock"
        ;;
    schema17)
        "$PREFLIGHT_SCHEMA17_BRIDGE" check --db "$PANEL_DB" \
            || die "restored schema17 panel database is not exact schema version 17"
        [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
            || die "schema17 rollback unexpectedly restored an agent ledger"
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock \
            || die "restored schema17 agent state is not idle under the release lock"
        ;;
esac

find "$BIN_DIR" "$WEB_DIR" -type f -exec sync -f -- {} \; \
    || die "restored binary/web files could not be made durable"
sync -f -- "$BIN_DIR" "$WEB_DIR" "$PANEL_DB" "$(dirname "$PANEL_DB")" \
    "$UNIT_DIR/celikpanel-agent.service" "$UNIT_DIR/celikpanel-panel.service" "$UNIT_DIR" \
    || die "restored release layout could not be made durable"
if [[ "$firewall_state" == present ]]; then
    sync -f -- "$UNIT_DIR/celikpanel-firewall-restore.service" \
        || die "restored firewall unit could not be made durable"
fi
if [[ "$agent_ledger_state" == present ]]; then
    sync -f -- "$AGENT_LEDGER" "$AGENT_STATE_DIR" \
        || die "restored agent ledger could not be made durable"
fi

if [[ $rollback_pending_resume -eq 0 ]]; then
    release_txn_mark_completion_pending \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "cannot mark rollback completion pending"
fi
release_txn_create_start_authorization \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$RELEASE_TRANSACTION_FD" "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "cannot authorize controlled rollback starts"

# Agent starts before panel only when their saved active-like states require it.
# Yalnız kayıtlı active-benzeri durumları gerektiriyorsa agent panelden önce başlar.
if service_state_is_active_like "${active_states[celikpanel-agent.service]}"; then
    release_release_mutation_lock \
        || die "cannot hand the mutation lock to the restored agent"
    systemctl start celikpanel-agent.service || die "restored agent did not start"
    for _ in $(seq 1 20); do
        [[ -S /run/celikpanel/agent.sock ]] && break
        sleep 0.3
    done
    [[ -S /run/celikpanel/agent.sock ]] || die "restored agent socket did not appear"
    acquire_release_mutation_lock handoff
    verify_restored_agent_idle_under_release_lock
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
        || die "rollback completion marker changed during the startup lock handoff"
fi
if service_state_is_active_like "${active_states[celikpanel-panel.service]}"; then
    systemctl start celikpanel-panel.service || die "restored panel did not start"
fi
for unit in celikpanel-agent.service celikpanel-panel.service; do
    verify_restored_service_active_state "$unit" "${active_states[$unit]}"
done

# Recheck the canonical database after an active-like panel has executed. That
# controlled start may leave a healthy non-empty SQLite WAL, so this proof must
# use the trusted WAL-aware checker before restoring the saved runtime state.
# Active-benzeri panel çalıştıktan sonra kanonik veritabanını yeniden denetle.
# Kontrollü başlangıç sağlıklı ve dolu bir SQLite WAL bırakabileceğinden kayıtlı
# çalışma durumunu geri getirmeden önce güvenilir WAL-aware checker kullanılır.
if service_state_is_active_like "${active_states[celikpanel-panel.service]}"; then
    systemctl stop celikpanel-panel.service \
        || die "restored panel could not be stopped for final database proof"
fi
case "$transition_state" in
    normal)
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$PREFLIGHT_PANEL" --check-service-operations-idle-wal-aware \
            || die "restored normal panel database changed during controlled start"
        ;;
    pre-ledger)
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle-wal-aware \
            || die "restored pre-ledger panel database changed during controlled start"
        ;;
    schema17)
        "$PREFLIGHT_SCHEMA17_BRIDGE" check --db "$PANEL_DB" \
            || die "restored schema17 panel database changed during controlled start"
        ;;
esac
sync -f -- "$PANEL_DB" "$(dirname "$PANEL_DB")" \
    || die "restored panel database final state could not be made durable"
if service_state_is_active_like "${active_states[celikpanel-panel.service]}"; then
    systemctl start celikpanel-panel.service || die "restored panel did not restart"
fi
for unit in celikpanel-agent.service celikpanel-panel.service; do
    verify_restored_service_active_state "$unit" "${active_states[$unit]}"
done
verify_restored_agent_idle_under_release_lock

release_txn_remove_start_authorization \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$RELEASE_TRANSACTION_FD" "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "cannot remove controlled rollback start authorization"
release_txn_validate_pending_token \
    "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "rollback completion marker changed before scheduler publication"
rollback_completion_verified=1
release_txn_mark_scheduler_restore_pending \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "cannot durably record pending rollback scheduler restoration"
rollback_scheduler_restore_pending=1
release_txn_validate_scheduler_restore_token \
    "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "rollback scheduler marker changed before completion removal"
rollback_completion_removing=1
release_txn_remove_completion_pending \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "cannot durably complete rollback transaction"
rollback_transaction_started=0
rollback_mutation_started=0
rollback_completion_removing=0
rollback_completion_verified=0
release_release_mutation_lock || die "cannot release rollback mutation lock"
panel_tls_quiesce_certbot_scheduler "$snap/panel-tls" \
    || die "Certbot renewal scheduler could not be re-quiesced before restoration"
release_txn_validate_scheduler_restore_token \
    "$RELEASE_TRANSACTION_ROOT" "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "rollback scheduler marker changed before restoration"
panel_tls_restore_certbot_scheduler "$snap/panel-tls" \
    || die "Certbot renewal scheduler state could not be restored"
rollback_scheduler_restore_completed=1
release_txn_remove_scheduler_restore_pending \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$rollback_transaction_token" rollback "$snapshot_name" \
    || die "cannot remove the exact rollback scheduler marker"
rollback_scheduler_restore_pending=0
commit=$(tr -d '[:space:]' < "$snap/commit")
trap - EXIT
echo
echo "==> Rollback complete / Geri alma tamamlandı"
echo "    Artifact source commit / Ürün kaynak commit'i: $commit"
echo "    Source checkout was not changed / Kaynak çalışma ağacı değiştirilmedi"
echo "    Panel: $(systemctl is-active celikpanel-panel.service 2>/dev/null || true)"
echo "    Agent: $(systemctl is-active celikpanel-agent.service 2>/dev/null || true)"
