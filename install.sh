#!/bin/bash
#
# CelikPanel installer — one command on a Linux host whose fixed-path systemd
# and package toolchain capabilities pass fail-closed preflight. Install
# tagged prebuilt releases; existing installations use the reviewed updater.
#
# CelikPanel kurulumu — sabit-yol systemd ve paket araç zinciri yetenekleri
# fail-closed ön kontrolden geçen Linux sunucuda giriş ekranına tek komut. Etiketli,
# önceden derlenmiş sürümü kurun; mevcut kurulumlarda incelenmiş updater'ı kullanın.
#
#   sudo ./install.sh
#
# Environment knobs / Ortam ayarları:
#   SKIP_DEPS=1     do not install prerequisites (tar, xz, curl, iproute2)
#   SKIP_ADMIN=1    do not prompt to create the first administrator
#   LISTEN=:2083    panel bind address
#   DEMO=1          R&D mode: quick-login accounts on the login screen
#                   (admin/reseller/customer @ demo1234) and cookies that
#                   work over plain HTTP. NEVER on an internet-facing server.
#                   AR-GE modu: giriş ekranında hızlı-giriş hesapları ve düz
#                   HTTP'de çalışan çerezler. İnternete açık sunucuda ASLA.

set -euo pipefail

# Ignore caller-controlled command lookup before privileged installation.
# Ayrıcalıklı kurulumdan önce çağıranın denetlediği komut arama yolunu yok say.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

PREFIX=/opt/celikpanel
DATA_DIR=/var/lib/celikpanel
IMPORT_DIR=/var/lib/celikpanel-imports
CONF_DIR=/etc/celikpanel
UNIT_DIR=/etc/systemd/system
PANEL_ENV="$CONF_DIR/panel.env"
INSTALL_COMPLETE=/etc/celikpanel/install.complete
PANEL_CERT_HOOK=/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert
AGENT_STATE_DIR=/var/lib/celikpanel-agent-private
AGENT_LEDGER="$AGENT_STATE_DIR/service-mutations.json"
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
RUNTIME_DIR=/run/celikpanel
BACKUP_ROOT=/var/backups/celikpanel
RELEASE_TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
RELEASE_TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
RELEASE_TRANSACTION_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
LIBEXEC_DIR=/usr/libexec/celikpanel
RELEASE_UPDATER=/usr/libexec/celikpanel/get.sh
RELEASE_PUBLIC_KEY=/etc/celikpanel/release-signing-ed25519.pem
RELEASE_STATE_DIR=/var/lib/celikpanel-release-state
SIGNED_UPDATE_LOCK="$RELEASE_STATE_DIR/update.lock"
readonly PREFIX DATA_DIR IMPORT_DIR CONF_DIR UNIT_DIR PANEL_CERT_HOOK \
    AGENT_STATE_DIR RUNTIME_DIR BACKUP_ROOT RELEASE_TRANSACTION_ROOT \
    RELEASE_TRANSACTION_RUNTIME_ROOT RELEASE_TRANSACTION_HELPER LIBEXEC_DIR \
    RELEASE_UPDATER RELEASE_PUBLIC_KEY RELEASE_STATE_DIR SIGNED_UPDATE_LOCK
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
SETPRIV_BIN=/usr/bin/setpriv
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
    DPKG_QUERY_BIN PACMAN_BIN TIMEOUT_BIN SETPRIV_BIN SELINUX_RESTORECON_BIN \
    SELINUX_MATCHPATHCON_BIN SELINUX_GETENFORCE_BIN UNAME_BIN VENDOR_READLINK_BIN \
    VENDOR_STAT_BIN VENDOR_DIRNAME_BIN SYSTEMCTL_BIN SYSTEMD_RUNTIME_DIR \
    SYSTEMD_PRIVATE_SOCKET VENDOR_TRUST_ANCHOR \
    VENDOR_EXPECTED_UID VENDOR_EXPECTED_GID
SELINUX_PLATFORM_MODE=unverified
SVC_USER=celikpanel
SVC_GROUP=celikpanel
LISTEN="${LISTEN:-:2083}"
case "${CELIKPANEL_APPLY_ONLY:-0}" in
    0|1) ;;
    *) printf '%s\n' "ERROR / HATA: CELIKPANEL_APPLY_ONLY must be 0 or 1 / yalnız 0 veya 1 olabilir" >&2; exit 1 ;;
esac
APPLY_ONLY=${CELIKPANEL_APPLY_ONLY:-0}
case "${DEMO:-0}" in
    0|1) ;;
    *) printf '%s\n' "ERROR / HATA: DEMO must be 0 or 1 / yalnız 0 veya 1 olabilir" >&2; exit 1 ;;
esac

SRC="$(cd "$(/usr/bin/dirname "$(/usr/bin/readlink -f "$0")")" && pwd -P)"
PANEL_TLS_DIR="$DATA_DIR/tls"

# Fresh self-signed certificates are created by the unprivileged panel process.
# Normalize their metadata once, after the service is stopped, so the public
# transactional updater can later take an exact root-trusted snapshot.
# shellcheck source=deploy/panel-tls-snapshot.sh
source "$SRC/deploy/panel-tls-snapshot.sh"

c() { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
bilingual() {
    local english=$1 turkish=${2:-}
    if [[ -n "$turkish" ]]; then
        printf '%s / %s' "$english" "$turkish"
    else
        printf '%s' "$english"
    fi
}
step() { c '1;36' "==> $(bilingual "$@")"; }
ok() { c '32' "    ✓ $(bilingual "$@")"; }
warn() { c '33' "    $(bilingual "$@")"; }
die() { c '1;31' "ERROR / HATA: $(bilingual "$@")" >&2; exit 1; }

validate_release_key_source_directory_chain() {
    local current=$1 canonical owner group mode permissions
    canonical=$(readlink -e -- "$current") \
        || die "operator release-key source directory is unavailable"
    [[ "$canonical" == "$current" ]] \
        || die "operator release-key source directory is not canonical"
    while true; do
        [[ -d "$current" && ! -L "$current" ]] \
            || die "operator release-key source ancestor is unsafe: $current"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
            || die "cannot inspect operator release-key source ancestor"
        permissions=$((8#$mode))
        [[ "$owner:$group" == 0:0 ]] && (( (permissions & 0022) == 0 )) \
            || die "operator release-key source ancestors must be root:root and group/other non-writable"
        [[ "$current" == / ]] && break
        current=$(dirname -- "$current")
    done
}

install_reviewed_release_updater() {
    local source=$SRC/libexec/get.sh tmp owner group mode links key_source key_tmp \
        key_size key_permissions permissions key_source_fd key_fd_path path_identity fd_identity
    if [[ ! -e "$SRC/release.version" && ! -L "$SRC/release.version" &&
          -f "$SRC/download-portal/get.sh" && ! -L "$SRC/download-portal/get.sh" ]]; then
        source=$SRC/download-portal/get.sh
    fi
    [[ -f "$source" && ! -L "$source" ]] \
        || die "reviewed release updater is missing from the verified release"
    [[ ! -L "$LIBEXEC_DIR" ]] \
        || die "release updater directory must not be a symbolic link"
    if [[ -e "$LIBEXEC_DIR" ]]; then
        [[ -d "$LIBEXEC_DIR" ]] || die "release updater directory target is not a directory"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$LIBEXEC_DIR") \
            || die "cannot inspect the release updater directory"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] && (( (permissions & 0022) == 0 )) \
            || die "existing release updater directory metadata is unsafe"
    fi
    install -d -m 0755 -o root -g root -- "$LIBEXEC_DIR"
    if [[ -e "$RELEASE_UPDATER" || -L "$RELEASE_UPDATER" ]]; then
        [[ -f "$RELEASE_UPDATER" && ! -L "$RELEASE_UPDATER" ]] \
            || die "installed release updater target is unsafe"
    fi
    tmp=$(mktemp "$LIBEXEC_DIR/.get.sh.XXXXXXXX") \
        || die "cannot stage the reviewed release updater"
    if ! cp --no-preserve=mode,ownership,timestamps -- "$source" "$tmp" ||
       ! chown root:root -- "$tmp" || ! chmod 0755 -- "$tmp" ||
       ! cmp -s -- "$source" "$tmp" || ! sync -f -- "$tmp" ||
       ! mv -T -- "$tmp" "$RELEASE_UPDATER" || ! sync -f -- "$LIBEXEC_DIR" ||
       ! cmp -s -- "$source" "$RELEASE_UPDATER"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        die "reviewed release updater could not be published exactly"
    fi
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$RELEASE_UPDATER") \
        || die "cannot inspect the installed release updater"
    [[ "$owner:$group:$mode:$links" == 0:0:755:1 ]] \
        || die "installed release updater metadata is unsafe"

    key_source=${CELIKPANEL_RELEASE_PUBLIC_KEY_FILE:-}
    [[ -n "$key_source" ]] || return 0
    [[ -f "$key_source" && ! -L "$key_source" ]] \
        || die "operator-provided release public key is not a regular file"
    [[ "$(readlink -e -- "$key_source")" == "$key_source" ]] \
        || die "operator-provided release public key path is not canonical"
    validate_release_key_source_directory_chain "$(dirname -- "$key_source")"
    exec {key_source_fd}<"$key_source" \
        || die "cannot open operator-provided release public key"
    key_fd_path=/proc/self/fd/$key_source_fd
    path_identity=$(stat -Lc '%d:%i' -- "$key_source") \
        || die "cannot identify operator-provided release public key path"
    fd_identity=$(stat -Lc '%d:%i' -- "$key_fd_path") \
        || die "cannot identify opened release public key"
    [[ "$path_identity" == "$fd_identity" ]] \
        || die "operator-provided release public key changed while opening"
    read -r owner group mode links key_size < <(stat -Lc '%u %g %a %h %s' -- "$key_fd_path") \
        || die "cannot inspect operator-provided release public key"
    [[ "$owner" == 0 && "$links" == 1 && "$key_size" -ge 1 && "$key_size" -le 16384 ]] \
        || die "operator-provided release public key metadata is unsafe"
    key_permissions=$((8#$mode))
    (( (key_permissions & 0022) == 0 )) \
        || die "operator-provided release public key must not be group/other writable"
    command -v openssl >/dev/null 2>&1 \
        || die "openssl is required to provision a release public key"
    key_tmp=$(mktemp "$CONF_DIR/.release-signing-ed25519.pem.XXXXXXXX") \
        || die "cannot stage the release public key"
    if ! cp --no-preserve=mode,ownership,timestamps -- "$key_fd_path" "$key_tmp" ||
       ! chown root:root -- "$key_tmp" || ! chmod 0644 -- "$key_tmp" ||
       ! cmp -s -- "$key_fd_path" "$key_tmp" || ! sync -f -- "$key_tmp"; then
        [[ ! -e "$key_tmp" && ! -L "$key_tmp" ]] || rm -f -- "$key_tmp"
        die "operator-provided release public key could not be staged exactly"
    fi
    openssl pkey -pubin -passin pass: -in "$key_tmp" -pubout 2>/dev/null \
        | cmp -s - "$key_tmp" \
        || die "operator-provided release public key must be canonical PEM"
    openssl pkey -pubin -passin pass: -in "$key_tmp" -text -noout 2>/dev/null \
        | LC_ALL=C grep -Eq '^ED25519 Public-Key:' \
        || die "operator-provided release public key must be Ed25519"
    if [[ -e "$RELEASE_PUBLIC_KEY" || -L "$RELEASE_PUBLIC_KEY" ]]; then
        [[ -f "$RELEASE_PUBLIC_KEY" && ! -L "$RELEASE_PUBLIC_KEY" ]] \
            || die "installed release public key target is unsafe"
        [[ "$(stat -Lc '%u:%g:%a:%h' -- "$RELEASE_PUBLIC_KEY")" == 0:0:644:1 ]] \
            || die "installed release public key metadata is unsafe"
        cmp -s -- "$key_tmp" "$RELEASE_PUBLIC_KEY" \
            || die "installed release public key differs; automatic replacement is refused"
        rm -f -- "$key_tmp"
        exec {key_source_fd}<&-
        return 0
    fi
    if ! mv -T -- "$key_tmp" "$RELEASE_PUBLIC_KEY" || ! sync -f -- "$CONF_DIR" ||
       ! cmp -s -- "$key_fd_path" "$RELEASE_PUBLIC_KEY"; then
        [[ ! -e "$key_tmp" && ! -L "$key_tmp" ]] || rm -f -- "$key_tmp"
        die "operator-provided release public key could not be published exactly"
    fi
    exec {key_source_fd}<&-
}

validate_release_state_parent_chain() {
    local current=/var/lib canonical owner group mode permissions
    canonical=$(readlink -e -- "$current") \
        || die "release state parent directory is unavailable"
    [[ "$canonical" == "$current" ]] \
        || die "release state parent directory is not canonical"
    while true; do
        [[ -d "$current" && ! -L "$current" ]] \
            || die "release state parent ancestor is unsafe: $current"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
            || die "cannot inspect release state parent ancestor"
        permissions=$((8#$mode))
        [[ "$owner:$group" == 0:0 ]] && (( (permissions & 0022) == 0 )) \
            || die "release state parent ancestors must be root:root and group/other non-writable"
        [[ "$current" == / ]] && break
        current=$(dirname -- "$current")
    done
}

provision_signed_update_lock() {
    local owner group mode links size lock_fd lock_fd_path path_identity fd_identity \
        effective_gid state_created=0 lock_created=0
    effective_gid=$(id -g) || die "cannot determine effective group for lock provisioning"
    validate_release_state_parent_chain

    if [[ ! -e "$RELEASE_STATE_DIR" && ! -L "$RELEASE_STATE_DIR" ]]; then
        if mkdir -m 0700 -- "$RELEASE_STATE_DIR" 2>/dev/null; then
            state_created=1
        elif [[ ! -d "$RELEASE_STATE_DIR" || -L "$RELEASE_STATE_DIR" ]]; then
            die "cannot provision release state directory"
        fi
    fi
    [[ -d "$RELEASE_STATE_DIR" && ! -L "$RELEASE_STATE_DIR" ]] \
        || die "release state directory is unsafe"
    [[ "$(readlink -e -- "$RELEASE_STATE_DIR")" == "$RELEASE_STATE_DIR" ]] \
        || die "release state directory is not canonical"
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
        sync -f -- "$RELEASE_STATE_DIR" \
            || die "cannot make release state directory durable"
        sync -f -- "$(dirname -- "$RELEASE_STATE_DIR")" \
            || die "cannot make release state directory entry durable"
    fi

    if [[ ! -e "$SIGNED_UPDATE_LOCK" && ! -L "$SIGNED_UPDATE_LOCK" ]]; then
        if (umask 077; set -o noclobber; : > "$SIGNED_UPDATE_LOCK") 2>/dev/null; then
            lock_created=1
        elif [[ ! -e "$SIGNED_UPDATE_LOCK" && ! -L "$SIGNED_UPDATE_LOCK" ]]; then
            die "cannot atomically provision signed update lock"
        fi
    fi
    [[ -f "$SIGNED_UPDATE_LOCK" && ! -L "$SIGNED_UPDATE_LOCK" ]] \
        || die "signed update lock is unsafe"
    [[ "$(readlink -e -- "$SIGNED_UPDATE_LOCK")" == "$SIGNED_UPDATE_LOCK" ]] \
        || die "signed update lock is not canonical"

    exec {lock_fd}<>"$SIGNED_UPDATE_LOCK" || die "cannot open signed update lock"
    lock_fd_path=/proc/self/fd/$lock_fd
    path_identity=$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK") \
        || die "cannot identify signed update lock path"
    fd_identity=$(stat -Lc '%d:%i' -- "$lock_fd_path") \
        || die "cannot identify opened signed update lock"
    [[ "$path_identity" == "$fd_identity" ]] \
        || die "signed update lock changed while opening"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$lock_fd_path") \
        || die "cannot inspect signed update lock"
    [[ "$owner" == 0 && "$mode" == 600 && "$links" == 1 && "$size" == 0 &&
       ( "$group" == 0 || "$group" == "$effective_gid" ) ]] \
        || die "signed update lock metadata is unsafe"
    if [[ "$group" != 0 ]]; then
        [[ "$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")" == "$fd_identity" ]] \
            || die "signed update lock changed before ownership recovery"
        chown root:root -- "$SIGNED_UPDATE_LOCK" \
            || die "cannot recover signed update lock ownership"
        lock_created=1
    fi
    sync -f -- "$SIGNED_UPDATE_LOCK" || die "cannot make signed update lock durable"
    sync -f -- "$RELEASE_STATE_DIR" || die "cannot make signed update lock entry durable"
    [[ "$(stat -Lc '%d:%i' -- "$SIGNED_UPDATE_LOCK")" == "$fd_identity" ]] \
        || die "signed update lock changed while provisioning"
    [[ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$lock_fd_path")" == 0:0:600:1:0 ]] \
        || die "signed update lock metadata could not be normalized"
    exec {lock_fd}>&-
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
        setpriv) VENDOR_TOOL_PATH=$SETPRIV_BIN; VENDOR_TOOL_ALLOWED_ALT= ;;
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

# Keep the established SELinux call sites explicit while sharing the same
# fixed-path verifier with package and systemd capability discovery.
validate_rhel_vendor_tool() {
    validate_vendor_tool "$1"
}

validate_present_platform_tools() {
    local role
    validate_systemd_runtime
    validate_vendor_tool setpriv
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

# Parse os-release as inert metadata without sourcing it. ID and ID_LIKE can
# disambiguate which complete vendor toolchain is expected; neither field nor
# VERSION_ID authorizes a mutation.
parse_bootstrap_os_release_scalar() {
    local raw=$1 field=$2 value
    case "$raw" in
        \"*)
            [[ ${#raw} -ge 2 && "${raw: -1}" == '"' ]] \
                || die "malformed quoted $field in /etc/os-release"
            value=${raw:1:${#raw}-2}
            [[ "$value" != *'"'* && "$value" != *\\* ]] \
                || die "unsupported escape in $field in /etc/os-release"
            ;;
        \'*)
            [[ ${#raw} -ge 2 && "${raw: -1}" == "'" ]] \
                || die "malformed quoted $field in /etc/os-release"
            value=${raw:1:${#raw}-2}
            [[ "$value" != *"'"* && "$value" != *\\* ]] \
                || die "unsupported escape in $field in /etc/os-release"
            ;;
        *)
            [[ "$raw" != *'"'* && "$raw" != *"'"* && "$raw" != *\\* ]] \
                || die "malformed $field in /etc/os-release"
            value=$raw
            ;;
    esac
    BOOTSTRAP_OS_RELEASE_VALUE=$value
}

parse_bootstrap_os_release() {
    local file=$1 line key raw
    local -A seen=()
    BOOTSTRAP_DISTRO_ID=
    BOOTSTRAP_DISTRO_VERSION_ID=
    BOOTSTRAP_DISTRO_ID_LIKE=
    [[ -f "$file" ]] \
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
                parse_bootstrap_os_release_scalar "$raw" "$key"
                case "$key" in
                    ID) BOOTSTRAP_DISTRO_ID=$BOOTSTRAP_OS_RELEASE_VALUE ;;
                    VERSION_ID) BOOTSTRAP_DISTRO_VERSION_ID=$BOOTSTRAP_OS_RELEASE_VALUE ;;
                    ID_LIKE) BOOTSTRAP_DISTRO_ID_LIKE=$BOOTSTRAP_OS_RELEASE_VALUE ;;
                esac
                ;;
        esac
    done < "$file"
    [[ "$BOOTSTRAP_DISTRO_ID" =~ ^[a-z0-9][a-z0-9._-]*$ ]] \
        || die "missing or invalid ID in operating-system identity file: $file"
    [[ -z "$BOOTSTRAP_DISTRO_VERSION_ID" ||
       "$BOOTSTRAP_DISTRO_VERSION_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._:+-]*$ ]] \
        || die "invalid VERSION_ID in operating-system identity file: $file"
    [[ -z "$BOOTSTRAP_DISTRO_ID_LIKE" ||
       "$BOOTSTRAP_DISTRO_ID_LIKE" =~ ^[a-z0-9][a-z0-9._-]*(\ [a-z0-9][a-z0-9._-]*)*$ ]] \
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

select_bootstrap_package_ecosystem() {
    local token hint candidate selected= combined
    local -A hints=()
    local -a complete=()

    validate_present_platform_tools
    combined="$BOOTSTRAP_DISTRO_ID $BOOTSTRAP_DISTRO_ID_LIKE"
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

classify_bootstrap_platform() {
    local os_release=$1 machine=$2
    SELINUX_PLATFORM_MODE=unverified
    parse_bootstrap_os_release "$os_release"
    case "$machine" in
        x86_64) BOOTSTRAP_ARCH=amd64 ;;
        aarch64) BOOTSTRAP_ARCH=arm64 ;;
        *) die "unsupported bootstrap architecture: $machine" ;;
    esac

    select_bootstrap_package_ecosystem
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
        || die "DNF preview requires SELinux Enforcing; SELinux state is unavailable"
    IFS= read -r enforcing < "$SELINUX_ENFORCE_FILE" \
        || die "DNF preview could not read the SELinux enforcement state"
    [[ "$enforcing" == 1 ]] \
        || die "DNF preview requires SELinux Enforcing and will not change host policy"
    validate_rhel_vendor_tool dnf
    validate_rhel_vendor_tool rpm
    validate_rhel_vendor_tool restorecon
    validate_rhel_vendor_tool matchpathcon
    validate_rhel_vendor_tool getenforce
    reported_state=$("$SELINUX_GETENFORCE_BIN") \
        || die "DNF preview could not query SELinux through $SELINUX_GETENFORCE_BIN"
    [[ "$reported_state" == Enforcing ]] \
        || die "DNF preview requires getenforce to report Enforcing"
}

# Pure dry-run description of the future prerequisite transaction. The normal
# installer does not execute this command until panel and agent activation has
# passed a complete SELinux-Enforcing lifecycle acceptance test.
rhel_preview_prerequisite_command() {
    printf '%s\n' "$RHEL_DNF_BIN" --assumeyes --setopt=install_weak_deps=False \
        install tar xz curl ca-certificates selinux-policy-targeted \
        policycoreutils libselinux-utils
}

preflight_bootstrap_platform() {
    local os_release=$1 machine=$2
    classify_bootstrap_platform "$os_release" "$machine"
    verify_live_selinux_preflight
    if [[ "$SELINUX_PLATFORM_MODE" == dnf-preview ]]; then
        verify_rhel_preview_host
        die "DNF bootstrap remains preview-only: package capability is verified, but the SELinux lifecycle is not implemented; no host changes were made"
    fi
    if [[ $APPLY_ONLY -eq 1 ]]; then
        PKG_FAMILY=apply-only
        return
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
        "$RELEASE_PUBLIC_KEY" \
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

# The trusted guard helper publishes its helper/drop-ins and then calls
# systemctl daemon-reload internally. Interpose only that bounded call so RHEL
# labels are restored after publication but before systemd reads the bytes.
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
    return "$status"
}

valid_panel_listen() {
    local value=$1 host port
    case "$value" in
        :*)
            host=
            port=${value#:}
            ;;
        \[*\]:*)
            host=${value%:*}
            port=${value##*:}
            [[ "$host" =~ ^\[[0-9A-Fa-f:]+\]$ ]] || return 1
            ;;
        *:*)
            host=${value%:*}
            port=${value##*:}
            [[ "$host" =~ ^[A-Za-z0-9._-]+$ ]] || return 1
            ;;
        *) return 1 ;;
    esac
    [[ "$port" =~ ^[0-9]{1,5}$ ]] || return 1
    (( 10#$port >= 1 && 10#$port <= 65535 ))
}

valid_panel_config_path() {
    local value=$1
    [[ "$value" =~ ^/[A-Za-z0-9._/@+-]+$ &&
       "$value" != *[[:space:]]* &&
       "$value" != *'/../'* && "$value" != */.. &&
       "$value" != *'/./'* && "$value" != */. ]]
}

# Validate without sourcing: panel.env is data, never shell code. The strict
# key set also catches typos before systemd restarts the public panel.
# source etmeden doğrula: panel.env kabuk kodu değil veridir. Sıkı anahtar
# kümesi, systemd açık paneli yeniden başlatmadan önce yazım hatalarını yakalar.
validate_panel_env() {
    local file=$1 line key value line_number=0
    local listen= tls= cert= key_path= tls_dir= cookie_flag= demo_flag=
    declare -A seen=()

    [[ -f "$file" && ! -L "$file" ]] || die "panel ortam dosyası eksik veya güvensiz: $file"
    read -r env_owner env_group env_mode < <(stat -Lc '%u %g %a' -- "$file") \
        || die "panel ortam dosyası incelenemedi"
    [[ "$env_owner" == 0 && "$env_group" == 0 && "$env_mode" == 600 ]] \
        || die "panel ortam dosyası root:root mode 0600 olmalı"

    while IFS= read -r line || [[ -n "$line" ]]; do
        ((line_number += 1))
        case "$line" in ''|'#'*) continue ;; esac
        [[ "$line" == *=* ]] || die "panel.env:$line_number geçersiz satır"
        key=${line%%=*}
        value=${line#*=}
        [[ -z "${seen[$key]+set}" ]] || die "panel.env:$line_number yinelenen anahtar: $key"
        seen[$key]=1
        case "$key" in
            CELIKPANEL_LISTEN)
                valid_panel_listen "$value" || die "panel.env:$line_number geçersiz dinleme adresi"
                listen=$value
                ;;
            CELIKPANEL_TLS)
                [[ "$value" == 0 || "$value" == 1 ]] || die "panel.env:$line_number CELIKPANEL_TLS 0 veya 1 olmalı"
                tls=$value
                ;;
            CELIKPANEL_TLS_CERT)
                valid_panel_config_path "$value" || die "panel.env:$line_number geçersiz sertifika yolu"
                cert=$value
                ;;
            CELIKPANEL_TLS_KEY)
                valid_panel_config_path "$value" || die "panel.env:$line_number geçersiz özel anahtar yolu"
                key_path=$value
                ;;
            CELIKPANEL_TLS_DIR)
                valid_panel_config_path "$value" || die "panel.env:$line_number geçersiz TLS dizini"
                tls_dir=$value
                ;;
            CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG)
                [[ -z "$value" || "$value" == --insecure-cookies ]] \
                    || die "panel.env:$line_number geçersiz cookie bayrağı"
                cookie_flag=$value
                ;;
            CELIKPANEL_PANEL_DEMO_FLAG)
                [[ -z "$value" || "$value" == --demo ]] \
                    || die "panel.env:$line_number geçersiz demo bayrağı"
                demo_flag=$value
                ;;
            *) die "panel.env:$line_number desteklenmeyen anahtar: $key" ;;
        esac
    done < "$file"

    [[ -n "${seen[CELIKPANEL_LISTEN]+set}" &&
       -n "${seen[CELIKPANEL_TLS]+set}" &&
       -n "${seen[CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG]+set}" &&
       -n "${seen[CELIKPANEL_PANEL_DEMO_FLAG]+set}" ]] \
        || die "panel.env zorunlu anahtarları eksik"
    [[ (-n "$cert" && -n "$key_path") || (-z "$cert" && -z "$key_path") ]] \
        || die "panel.env sertifika ve özel anahtar yollarını birlikte tanımlamalı"
    if [[ -n "$demo_flag" ]]; then
        [[ "$cookie_flag" == --insecure-cookies && "$tls" == 0 && -z "$cert" ]] \
            || die "demo modu yalnız güvensiz cookie + TLS kapalı bileşimiyle kullanılabilir"
    else
        [[ -z "$cookie_flag" ]] || die "güvensiz cookie yalnız demo modunda kullanılabilir"
        [[ "$tls" == 1 || -n "$cert" ]] || die "normal panel modu TLS olmadan çalıştırılamaz"
    fi

    VALIDATED_PANEL_LISTEN=$listen
    VALIDATED_PANEL_HTTPS=1
    [[ -n "$demo_flag" ]] && VALIDATED_PANEL_HTTPS=0
    : "$tls_dir"
}

legacy_panel_unit_value() {
    local file=$1 key=$2 prefix="Environment=$2=" line value= count=0
    while IFS= read -r line; do
        [[ "$line" == "$prefix"* ]] || continue
        value=${line#"$prefix"}
        ((count += 1))
    done < "$file"
    (( count <= 1 )) || die "eski panel unitinde yinelenen $key ayarı"
    printf '%s' "$value"
}

ensure_panel_env() {
    local installed_unit=/etc/systemd/system/celikpanel-panel.service
    local listen=$LISTEN tls=1 cert= key_path= tls_dir= cookie_flag= demo_flag= migrated=
    local unit_owner unit_group unit_mode unit_permissions old_exec temp_env

    if [[ -e "$PANEL_ENV" || -L "$PANEL_ENV" ]]; then
        validate_panel_env "$PANEL_ENV"
        return
    fi

    valid_panel_listen "$listen" || die "geçersiz LISTEN değeri: $listen"
    if [[ "${DEMO:-0}" == 1 ]]; then
        tls=0
        cookie_flag=--insecure-cookies
        demo_flag=--demo
    fi

    # Migrate the exact settings written by older installers before replacing
    # their generated unit. Unknown command-line overrides stop the upgrade
    # instead of being silently discarded.
    # Eski kurucunun yazdığı ayarları üretilmiş unit değiştirilmeden önce taşı.
    # Bilinmeyen komut satırı geçersiz kılmaları sessizce kaybolmak yerine
    # yükseltmeyi durdurur.
    if [[ -e "$installed_unit" || -L "$installed_unit" ]]; then
        [[ -f "$installed_unit" && ! -L "$installed_unit" ]] \
            || die "eski panel systemd unit yolu güvensiz"
        read -r unit_owner unit_group unit_mode < <(stat -Lc '%u %g %a' -- "$installed_unit") \
            || die "eski panel systemd uniti incelenemedi"
        unit_permissions=$((8#$unit_mode))
        [[ "$unit_owner" == 0 && "$unit_group" == 0 ]] \
            && (( (unit_permissions & 0022) == 0 )) \
            || die "eski panel systemd uniti root sahipli ve yazmaya kapalı olmalı"

        migrated=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_LISTEN)
        [[ -z "$migrated" ]] || listen=$migrated
        migrated=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS)
        [[ -z "$migrated" ]] || tls=$migrated
        cert=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS_CERT)
        key_path=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS_KEY)
        tls_dir=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS_DIR)
        old_exec=$(sed -n 's/^ExecStart=//p' "$installed_unit" | tail -n 1)
        case "$old_exec" in
            /opt/celikpanel/bin/panel|\
            '/opt/celikpanel/bin/panel $CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG $CELIKPANEL_PANEL_DEMO_FLAG'|\
            '') ;;
            '/opt/celikpanel/bin/panel --insecure-cookies --demo')
                tls=0
                cookie_flag=--insecure-cookies
                demo_flag=--demo
                ;;
            *) die "eski panel ExecStart ayarı otomatik taşınamıyor: $old_exec" ;;
        esac
    fi

    valid_panel_listen "$listen" || die "taşınan panel dinleme adresi geçersiz"
    [[ "$tls" == 0 || "$tls" == 1 ]] || die "taşınan TLS ayarı geçersiz"
    [[ -z "$cert" ]] || valid_panel_config_path "$cert" || die "taşınan sertifika yolu geçersiz"
    [[ -z "$key_path" ]] || valid_panel_config_path "$key_path" || die "taşınan özel anahtar yolu geçersiz"
    [[ -z "$tls_dir" ]] || valid_panel_config_path "$tls_dir" || die "taşınan TLS dizini geçersiz"

    temp_env=$(mktemp "$CONF_DIR/.panel.env.XXXXXXXX") || die "panel ortam geçici dosyası oluşturulamadı"
    chmod 0600 "$temp_env"
    chown root:root "$temp_env"
    {
        printf 'CELIKPANEL_LISTEN=%s\n' "$listen"
        printf 'CELIKPANEL_TLS=%s\n' "$tls"
        [[ -z "$cert" ]] || printf 'CELIKPANEL_TLS_CERT=%s\n' "$cert"
        [[ -z "$key_path" ]] || printf 'CELIKPANEL_TLS_KEY=%s\n' "$key_path"
        [[ -z "$tls_dir" ]] || printf 'CELIKPANEL_TLS_DIR=%s\n' "$tls_dir"
        printf 'CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG=%s\n' "$cookie_flag"
        printf 'CELIKPANEL_PANEL_DEMO_FLAG=%s\n' "$demo_flag"
    } > "$temp_env"
    mv -T --no-clobber -- "$temp_env" "$PANEL_ENV" \
        || { rm -f -- "$temp_env"; die "panel ortam dosyası yayınlanamadı"; }
    validate_panel_env "$PANEL_ENV"
}

service_group_id() {
    local group_id
    group_id=$(getent group "$SVC_GROUP" | cut -d: -f3) || return 1
    [[ "$group_id" =~ ^(0|[1-9][0-9]*)$ ]] || return 1
    printf '%s\n' "$group_id"
}

service_user_id() {
    local user_id
    user_id=$(getent passwd "$SVC_USER" | cut -d: -f3) || return 1
    [[ "$user_id" =~ ^(0|[1-9][0-9]*)$ ]] || return 1
    printf '%s\n' "$user_id"
}

# Revalidate the fixed util-linux identity switch immediately before every
# panel bootstrap command. Numeric identities and an empty supplementary-group
# set avoid NSS-dependent identity changes and inherited root groups.
run_panel_as_service_user_with_private_umask() {
    [[ "${SVC_USER_ID:-}" =~ ^[0-9]+$ && "$SVC_USER_ID" != 0 &&
       "${SVC_GROUP_ID:-}" =~ ^[0-9]+$ && "$SVC_GROUP_ID" != 0 ]] \
        || die "panel bootstrap service identity is invalid"
    validate_vendor_tool setpriv
    CELIKPANEL_DATA_DIR="$DATA_DIR" \
        "$SETPRIV_BIN" --reuid="$SVC_USER_ID" --regid="$SVC_GROUP_ID" \
        --clear-groups -- \
        /bin/sh -c 'umask 077; exec "$@"' celikpanel-install "$PREFIX/bin/panel" "$@"
}

ensure_first_administrator() {
    local admin_count
    [[ "${SKIP_ADMIN:-0}" != 1 ]] || return 0
    if ! admin_count=$(run_panel_as_service_user_with_private_umask --count-users); then
        die "Administrator count failed" "Yönetici sayısı alınamadı"
    fi
    [[ "$admin_count" =~ ^(0|[1-9][0-9]*)$ ]] \
        || die "Administrator count returned invalid data" "Yönetici sayısı geçersiz veri döndürdü"
    if [[ "$admin_count" == 0 ]]; then
        step "Creating the first administrator" "İlk yönetici oluşturuluyor"
        run_panel_as_service_user_with_private_umask --create-admin || \
            die "Administrator creation failed" "Yönetici oluşturma başarısız"
        ok "administrator is ready" "yönetici hazır"
        return 0
    fi
    ok "An administrator already exists — skipped" \
        "Yönetici zaten var — atlandı"
}

[ "$(/usr/bin/id -u)" -eq 0 ] || die "root olarak çalıştırın (sudo ./install.sh)"
bootstrap_machine=$(vendor_machine_architecture)
preflight_bootstrap_platform "$SELINUX_OS_RELEASE" "$bootstrap_machine"
for installer_command in chown chmod cmp cp flock install mktemp mv stat sync; do
    command -v "$installer_command" >/dev/null \
        || die "required installer command is unavailable: $installer_command"
done

# Apply-only is accepted solely from a completely verified immutable release
# while the inherited persistent lock and exact active update marker are live.
# Apply-only yalnız tamamen doğrulanmış değişmez sürümden, miras kalıcı kilit ve
# tam active update işaretçisi canlıyken kabul edilir.
validate_apply_only_transaction() {
    local root canonical relative entry owner mode permissions state
    [[ "$APPLY_ONLY" -eq 1 ]] || return 0
    [[ "${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}" == 0 ]] \
        || die "apply-only cannot initialize the service mutation ledger"
    [[ "${SKIP_DEPS:-}" == 1 && "${SKIP_SECURITY_UPDATES:-}" == 1 && "${SKIP_ADMIN:-}" == 1 ]] \
        || die "apply-only requires dependency, security-update and admin work disabled"
    [[ "${DEMO:-0}" != 1 ]] || die "apply-only refuses demo mode"
    root=${CELIKPANEL_TRUSTED_RELEASE_ROOT:-}
    [[ "$root" == "$SRC" && "$root" == /* ]] || die "apply-only trusted release root mismatch"
    canonical=$(readlink -e -- "$root") || die "apply-only trusted release is unavailable"
    [[ "$canonical" == "$root" ]] || die "apply-only trusted release path is not canonical"
    [[ "$root" == /var/backups/celikpanel/releases/* ]] || die "apply-only release is outside retained storage"
    relative=${root#/var/backups/celikpanel/releases/}
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] || die "apply-only release name is invalid"
    [[ -d "$root" && ! -L "$root" && ! -e "$root/.git" ]] || die "apply-only release root is unsafe"
    read -r owner mode < <(stat -Lc '%u %a' -- "$root") || die "cannot inspect apply-only release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] || die "apply-only release root must be root-owned mode 0700"
    if find "$root" -type l -print -quit | grep -q .; then die "apply-only release contains a symbolic link"; fi
    if find "$root" ! -type d ! -type f -print -quit | grep -q .; then die "apply-only release contains a special object"; fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") || die "cannot inspect apply-only release entry"
        [[ "$owner" == 0 ]] || die "apply-only release entry is not root-owned"
        permissions=$((8#$mode)); (( (permissions & 0022) == 0 )) || die "apply-only release entry is writable"
    done < <(find "$root" -mindepth 1 -print0)
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] || die "apply-only checksum manifest is missing"
    (cd "$root"; LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | cmp -s - SHA256SUMS; sha256sum -c SHA256SUMS >/dev/null) \
        || die "apply-only trusted release checksum verification failed"
    [[ -x "$root/bin/panel" && -x "$root/bin/agent" && -f "$root/web/dist/index.html" ]] \
        || die "apply-only release artifacts are incomplete"
    [[ "${CELIKPANEL_RELEASE_TRANSACTION_FD:-}" =~ ^[0-9]+$ ]] || die "apply-only transaction FD is missing"
    [[ "${CELIKPANEL_RELEASE_TRANSACTION_OPERATION:-}" == update ]] || die "apply-only operation must be update"
    TRUSTED_RELEASE_ROOT=$root
    # shellcheck source=deploy/release-transaction-guard.sh
    source "$root/deploy/release-transaction-guard.sh"
    release_txn_verify_inherited_lock "$RELEASE_TRANSACTION_ROOT" "$CELIKPANEL_RELEASE_TRANSACTION_FD" \
        || die "apply-only inherited transaction lock proof failed"
    release_txn_validate_active_token "$RELEASE_TRANSACTION_ROOT" \
        "${CELIKPANEL_RELEASE_TRANSACTION_TOKEN:-}" update \
        "${CELIKPANEL_RELEASE_TRANSACTION_SNAPSHOT:-}" \
        || die "apply-only active transaction marker proof failed"
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        state=$("$SYSTEMCTL_BIN" show --property=ActiveState --value "$unit") || die "cannot inspect $unit for apply-only"
        [[ "$state" == inactive || "$state" == failed ]] || die "apply-only requires $unit stopped"
    done
    install_release_transaction_guards_with_label_barrier \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
        /etc/systemd/system "$RELEASE_TRANSACTION_HELPER" \
        "$CELIKPANEL_RELEASE_TRANSACTION_FD" \
        || die "apply-only transaction service guard proof failed"
}
validate_apply_only_transaction

# Ledger initialization is one-shot. Permit it only for an explicitly audited
# pre-ledger transition or a proven fresh host with neither DB nor private state.
# Ledger başlatma tek seferliktir. Yalnız açıkça denetlenmiş pre-ledger geçişinde
# veya DB ve özel state bulunmayan kanıtlanmış temiz makinede izin ver.
case "${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}" in
    0|1) ;;
    *) die "INITIALIZE_SERVICE_MUTATION_LEDGER yalnız 0 veya 1 olabilir" ;;
esac
initialize_ledger=${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}
# Existing private state must already be the exact root-only directory. Reject
# aliases before install -d can follow or normalize them.
# Mevcut özel state önceden tam root-only dizin olmalıdır. install -d yolu
# izleyip normalleştirmeden önce alias'ları reddet.
if [[ -e "$AGENT_STATE_DIR" || -L "$AGENT_STATE_DIR" ]]; then
    [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]] \
        || die "güvensiz özel agent state yolu: $AGENT_STATE_DIR"
    read -r state_owner state_group state_mode < <(stat -Lc '%u %g %a' -- "$AGENT_STATE_DIR") \
        || die "özel agent state dizini incelenemedi"
    expected_state_group=$(service_group_id) \
        || die "mevcut özel agent state için $SVC_GROUP grubu bulunamadı"
    [[ "$state_owner" == 0 && "$state_group" == "$expected_state_group" && "$state_mode" == 700 ]] \
        || die "özel agent state dizini root:$SVC_GROUP mode 0700 olmalı"
fi
# A failed fresh install may have created only the exact empty private
# directory. Treat that one state as fresh so retry remains idempotent; any
# hidden or visible entry keeps automatic initialization fail-closed.
# Başarısız temiz kurulum yalnız tam boş özel dizini oluşturmuş olabilir.
# Yeniden deneme idempotent kalsın diye yalnız bu durumu temiz kabul et; gizli
# veya görünür herhangi bir girdi otomatik başlatmayı kapalı biçimde reddettirir.
if [[ ! -e "$DATA_DIR/celikpanel.db" && ! -L "$DATA_DIR/celikpanel.db" ]]; then
    if [[ ! -e "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]]; then
        initialize_ledger=1
    elif [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" &&
            ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
        partial_state_entry=$(find "$AGENT_STATE_DIR" -mindepth 1 -maxdepth 1 -print -quit) \
            || die "özel agent state dizini boşluğu doğrulanamadı"
        if [[ -z "$partial_state_entry" ]]; then
            initialize_ledger=1
        fi
    fi
fi
if [[ $initialize_ledger -eq 1 &&
      -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" &&
      ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
    unexpected_initial_state=$(find "$AGENT_STATE_DIR" -mindepth 1 -maxdepth 1 -print -quit) \
        || die "özel agent state içeriği doğrulanamadı"
    [[ -z "$unexpected_initial_state" ]] \
        || die "ledger başlatma yalnız tamamen boş özel agent state dizininde yapılabilir"
fi
if [[ -e "$AGENT_LEDGER" || -L "$AGENT_LEDGER" ]]; then
    [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || die "güvensiz servis işlem ledger yolu: $AGENT_LEDGER"
    read -r ledger_owner ledger_group ledger_mode < <(stat -Lc '%u %g %a' -- "$AGENT_LEDGER") \
        || die "servis işlem ledger incelenemedi"
    expected_ledger_group=$(service_group_id) \
        || die "mevcut servis işlem ledger için $SVC_GROUP grubu bulunamadı"
    [[ "$ledger_owner" == 0 && "$ledger_group" == "$expected_ledger_group" && "$ledger_mode" == 600 ]] \
        || die "servis işlem ledger root:$SVC_GROUP mode 0600 olmalı"
    [[ "${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}" != 1 ]] \
        || die "servis işlem ledger zaten var; explicit başlatma reddedildi"
elif [[ $initialize_ledger -ne 1 ]]; then
    die "servis işlem ledger eksik; yalnız temiz kurulum veya denetlenmiş bootstrap başlatabilir"
fi

# Revalidate the selected fixed-path toolchain immediately before the first
# package mutation. A foreign package manager neither authorizes nor redirects
# bootstrap; DNF preview hosts have already stopped above.
case "$PKG_FAMILY" in
    apply-only) ;;
    apt|pacman) validate_selected_package_ecosystem "$PKG_FAMILY" ;;
    *) die "internal bootstrap package-family error: $PKG_FAMILY" ;;
esac

# 1. Minimal prerequisites ---------------------------------------------------
# The panel and agent are self-contained (static Go binaries + embedded
# SQLite); we install NOTHING for hosting here. nginx / php / mariadb /
# postgresql / mail are added later from the panel, on demand, so the operator
# runs only what they actually want (constitution: what isn't installed is
# invisible). We ensure only the few tiny tools the agent itself uses.
#
# nftables and iproute2 belong in this list, not in the on-demand catalog. They
# provide the firewall and exact local-address inspection used by the agent.
# The
# kernel packet filter (netfilter) is always present; only this userspace `nft`
# binary can be missing on a minimal image. Installing it changes nothing:
# it writes no rules and closes no ports until the operator hits "Turn on", and
# it never conflicts with ufw / firewalld / a cloud firewall (those drive the
# same nftables underneath). Having the tool ≠ enabling the firewall — so the
# firewall stays a clean on/off switch and this respects "never turn on a
# firewall by surprise."
#
# Panel ve agent kendi kendine yeter (statik Go binary + gömülü SQLite);
# barındırma için burada HİÇBİR ŞEY kurmayız. nginx / php / mariadb /
# postgresql / mail sonradan panelden, talep üzerine eklenir; böylece operatör
# yalnız gerçekten istediğini çalıştırır. Yalnız agent'ın kendi kullandığı
# birkaç küçük aracı sağlarız.
#
# nftables bu listede olmalı, talep-üzerine katalogda değil. Agent'ın firewall
# için çağırdığı araç budur — tıpkı curl gibi tesisat. Çekirdek paket süzgeci
# (netfilter) hep vardır; minimal imajda eksik olabilen yalnız bu kullanıcı-
# alanı `nft` ikilisidir. Kurmak hiçbir şeyi değiştirmez: operatör "Turn on"
# demeden tek kural yazmaz, tek port kapatmaz ve ufw / firewalld / bulut
# firewall ile ASLA çakışmaz (hepsi altta aynı nftables'ı sürer). Aracı kurmak
# ≠ firewall'u açmak — böylece firewall temiz bir aç/kapa düğmesi kalır ve bu,
# "firewall'u sürprizle açma" kuralına uyar.
if [[ $APPLY_ONLY -eq 0 ]] && [ "${SKIP_DEPS:-0}" != "1" ]; then
    step "Small prerequisites (curl, tar, xz, nftables, iproute2)" \
        "Küçük ön gereksinimler (curl, tar, xz, nftables, iproute2)"
    case "$PKG_FAMILY" in
    apt)
        export DEBIAN_FRONTEND=noninteractive
        # A broken third-party repo must not abort the install; the packages we
        # need come from the base archives and may already be cached.
        # Bozuk bir üçüncü parti depo kurulumu iptal etmemeli; ihtiyacımız olan
        # paketler ana arşivlerden gelir ve zaten önbellekte olabilir.
        "$APT_GET_BIN" update -qq || warn "apt-get update returned a warning — continuing" \
            "apt-get update uyarı verdi — devam ediliyor"
        "$APT_GET_BIN" install -y -qq tar xz-utils curl ca-certificates nftables iproute2 >/dev/null
        ;;
    pacman)
        # The pacman ecosystem does not support partial upgrades. Refresh,
        # upgrade and install
        # prerequisites in one transaction so the host is never left with a
        # new package database and an old base system.
        # Arch kısmi yükseltmeleri desteklemez. Makineyi yeni paket veritabanı
        # ve eski temel sistemle bırakmamak için tazeleme, yükseltme ve ön
        # gereksinim kurulumunu tek işlemde yap.
        "$PACMAN_BIN" -Syu --noconfirm --needed tar xz curl ca-certificates nftables iproute2 >/dev/null
        ;;
    esac
    ok "ready" "hazır"
else
    step "Prerequisite installation skipped (SKIP_DEPS=1)" \
        "Ön gereksinim kurulumu atlandı (SKIP_DEPS=1)"
fi

# 1b. Automatic security patches --------------------------------------------
# Every package the operator later installs from the panel (nginx, postfix,
# PowerDNS…) is attack surface; unattended-upgrades keeps that surface patched
# without anyone remembering to. Security origin only — never feature upgrades,
# so a hosting box is never surprised by a behaviour change, only by a fix.
# SKIP_SECURITY_UPDATES=1 opts out.
#
# 1b. Otomatik güvenlik yamaları. Operatörün panelden kurduğu her paket
# (nginx, postfix, PowerDNS…) saldırı yüzeyidir; unattended-upgrades bu yüzeyi
# kimse hatırlamak zorunda kalmadan yamalı tutar. Yalnız güvenlik kaynağı —
# asla özellik yükseltmesi; barındırma kutusu davranış değişikliğiyle
# şaşırmaz, yalnız düzeltmeyle. SKIP_SECURITY_UPDATES=1 devre dışı bırakır.
if [[ $APPLY_ONLY -eq 0 ]] && [ "${SKIP_DEPS:-0}" != "1" ] && [ "${SKIP_SECURITY_UPDATES:-0}" != "1" ] && [ "$PKG_FAMILY" = "apt" ]; then
    step "Automatic security patches (unattended-upgrades)" \
        "Otomatik güvenlik yamaları (unattended-upgrades)"
    export DEBIAN_FRONTEND=noninteractive
    if "$APT_GET_BIN" install -y -qq unattended-upgrades >/dev/null 2>&1; then
        # Enable the periodic timer: update lists + apply security upgrades daily.
        # Periyodik zamanlayıcıyı aç: listeleri güncelle + günlük güvenlik yaması.
        cat > /etc/apt/apt.conf.d/20celikpanel-auto-upgrades <<'AUTOCONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
AUTOCONF
        "$SYSTEMCTL_BIN" enable --now unattended-upgrades >/dev/null 2>&1 || true
        ok "security patches enabled" "güvenlik yamaları etkin"
    else
        warn "unattended-upgrades could not be installed — skipped (it can be installed manually)" \
            "unattended-upgrades kurulamadı — atlandı (elle kurulabilir)"
    fi
elif [[ $APPLY_ONLY -eq 0 ]] && [ "${SKIP_DEPS:-0}" != "1" ] && [ "${SKIP_SECURITY_UPDATES:-0}" != "1" ] && [ "$PKG_FAMILY" = "pacman" ]; then
    # CelikPanel has no verified security-only channel for the pacman ecosystem,
    # so we say so instead of pretending.
    # CelikPanel'in pacman ekosistemi için doğrulanmış güvenlik-yalnız yama
    # kanalı yoktur; öyleymiş gibi yapmak yerine bunu söyleriz.
    step "Automatic security patches" "Otomatik güvenlik yamaları"
    warn "The pacman ecosystem has no managed security-only channel — automatic patches were not configured; keep the system current with 'pacman -Syu'" \
        "Pacman ekosisteminde yönetilen güvenlik-yalnız kanal yok — otomatik yama kurulmadı; sistemi 'pacman -Syu' ile güncel tutun"
fi

# 2. Service user & group ----------------------------------------------------
step "Service user and group" "Servis kullanıcısı ve grubu"
if [[ $APPLY_ONLY -eq 1 ]]; then
    getent group "$SVC_GROUP" >/dev/null || die "apply-only requires existing $SVC_GROUP group"
    id "$SVC_USER" >/dev/null 2>&1 || die "apply-only requires existing $SVC_USER user"
else
    getent group "$SVC_GROUP" >/dev/null || groupadd --system "$SVC_GROUP"
    if ! id "$SVC_USER" >/dev/null 2>&1; then
        useradd --system --gid "$SVC_GROUP" --home-dir "$DATA_DIR" \
            --shell /usr/sbin/nologin "$SVC_USER"
    fi
fi
SVC_GROUP_ID=$(service_group_id) || die "$SVC_GROUP group ID could not be resolved" \
    "$SVC_GROUP grup kimliği çözülemedi"
SVC_USER_ID=$(service_user_id) || die "$SVC_USER user ID could not be resolved" \
    "$SVC_USER kullanıcı kimliği çözülemedi"
ok "$SVC_USER:$SVC_GROUP"

# Validate or migrate durable operator choices before building or replacing any
# installed product bytes. A bad root-owned configuration must fail closed
# while the currently installed release is still untouched.
# Kalıcı operatör seçimlerini kurulu ürün baytlarını derlemeden veya
# değiştirmeden önce doğrula/taşı. Bozuk root-only yapılandırma, mevcut sürüme
# dokunulmadan güvenli biçimde kurulumu durdurmalıdır.
step "Panel configuration $PANEL_ENV" "Panel yapılandırması $PANEL_ENV"
if [[ -e "$CONF_DIR" || -L "$CONF_DIR" ]]; then
    [[ -d "$CONF_DIR" && ! -L "$CONF_DIR" ]] || die "panel yapılandırma dizini güvensiz"
    read -r conf_owner conf_group conf_mode < <(stat -Lc '%u %g %a' -- "$CONF_DIR") \
        || die "panel yapılandırma dizini incelenemedi"
    conf_permissions=$((8#$conf_mode))
    [[ "$conf_owner" == 0 ]] \
        && (( (conf_permissions & 0022) == 0 )) \
        || die "panel yapılandırma dizini root sahipli ve group/other yazılamaz olmalı"
fi
install -d -m 0750 -o root -g "$SVC_GROUP" "$CONF_DIR"
ensure_panel_env
ok "ready" "hazır"

# 3. Build if artifacts are missing ------------------------------------------
# A prebuilt release tarball already contains bin/ and web/dist, so this whole
# step is skipped there. From a bare git checkout we build from source,
# bootstrapping the Go and Node toolchains if the system lacks them — so
# "git clone && sudo ./install.sh" works on a stock Ubuntu with nothing else.
#
# Önceden derlenmiş bir release tarball zaten bin/ ve web/dist içerir; orada bu
# adım tümüyle atlanır. Çıplak bir git checkout'tan kaynaktan derleriz;
# sistemde yoksa Go ve Node araç zincirlerini indiririz — böylece stok bir
# Ubuntu'da başka hiçbir şey olmadan "git clone && sudo ./install.sh" çalışır.
GO_VERSION=1.26.5
NODE_VERSION=24.18.0
TOOLCHAIN=/opt/celikpanel/.toolchain
GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49
NODE_SHA256_AMD64=55aa7153f9d88f28d765fcdad5ae6945b5c0f98a36881703817e4c450fa76742
NODE_SHA256_ARM64=58c9520501f6ae2b52d5b210444e24b9d0c029a58c5011b797bc1fe7105886f6
TOOLCHAIN_ENV_BIN=/usr/bin/env
TOOLCHAIN_CURL_BIN=/usr/bin/curl
TOOLCHAIN_INSTALL_BIN=/usr/bin/install
TOOLCHAIN_MV_BIN=/usr/bin/mv
TOOLCHAIN_READLINK_BIN=/usr/bin/readlink
TOOLCHAIN_SHA256_BIN=/usr/bin/sha256sum
TOOLCHAIN_STAT_BIN=/usr/bin/stat
TOOLCHAIN_TAR_BIN=/usr/bin/tar

run_external_clean() {
    "$TOOLCHAIN_ENV_BIN" -i \
        HOME=/root \
        PATH=/usr/sbin:/usr/bin:/sbin:/bin \
        LC_ALL=C \
        LANG=C \
        "$@"
}

# Privileged source builds must not inherit Go, compiler or npm configuration
# from sudo's calling environment. Keep the allowlist explicit and disable
# per-user Go configuration/workspace discovery and CGO tool invocation.
run_go_clean() {
    run_external_clean \
        GOTOOLCHAIN=local \
        GOENV=off \
        GOWORK=off \
        GOPATH=/root/go \
        GOCACHE=/root/.cache/go-build \
        CGO_ENABLED=0 \
        "$@"
}

run_node_clean() {
    local node_bin_dir=$1
    shift
    "$TOOLCHAIN_ENV_BIN" -i \
        HOME=/root \
        PATH="$node_bin_dir:/usr/sbin:/usr/bin:/sbin:/bin" \
        LC_ALL=C \
        LANG=C \
        "$@"
}

# Downloaded toolchains are later trusted by the privileged updater. Archive
# metadata must never decide their owner, and an existing tree is reused only
# after every entry has passed the same ownership and write-permission checks.
validate_bootstrap_toolchain_tree() {
    local root=$1 canonical entry link_value target uid gid mode
    ensure_bootstrap_toolchain_root
    case "$root" in
        "$TOOLCHAIN/go"|"$TOOLCHAIN/node"|"$TOOLCHAIN"/.go-stage.*/go|"$TOOLCHAIN"/.node-stage.*) ;;
        *) die "refusing to validate unexpected toolchain path: $root" ;;
    esac
    [ -d "$root" ] && [ ! -L "$root" ] \
        || die "toolchain root must be a real directory: $root"
    canonical=$(readlink -e -- "$root") \
        || die "toolchain root is unavailable: $root"
    [[ "$canonical" == "$root" ]] \
        || die "toolchain root path is not canonical: $root"

    while IFS= read -r -d '' entry; do
        uid=$(stat -c '%u' -- "$entry")
        gid=$(stat -c '%g' -- "$entry")
        [ "$uid:$gid" = 0:0 ] \
            || die "toolchain entry must be owned by root:root: $entry"
        if [ -L "$entry" ]; then
            link_value=$(readlink -- "$entry") || die "cannot inspect toolchain symlink: $entry"
            [[ "$link_value" != /* ]] || die "absolute toolchain symlink is not relocatable: $entry"
            target=$(readlink -f -- "$entry") \
                || die "broken toolchain symlink: $entry"
            [[ "$target" == "$root"/* ]] \
                || die "toolchain symlink escapes its root: $entry"
            continue
        fi
        [ -f "$entry" ] || [ -d "$entry" ] \
            || die "unsupported object in toolchain: $entry"
        mode=$(stat -c '%a' -- "$entry")
        (( (8#$mode & 8#022) == 0 )) \
            || die "toolchain entry must not be group/other writable: $entry"
    done < <(find "$root" -xdev -print0)
}

seal_bootstrap_toolchain_tree() {
    local root=$1
    ensure_bootstrap_toolchain_root
    chown -R -h root:root -- "$root"
    chmod -R go-w -- "$root"
    validate_bootstrap_toolchain_tree "$root"
}

validate_bootstrap_trusted_directory() {
    local directory=$1 canonical uid gid mode
    [[ -d "$directory" && ! -L "$directory" ]] \
        || die "trusted toolchain directory must be a real directory: $directory"
    canonical=$("$TOOLCHAIN_READLINK_BIN" -e -- "$directory") \
        || die "trusted toolchain directory is unavailable: $directory"
    [[ "$canonical" == "$directory" ]] \
        || die "trusted toolchain directory path is not canonical: $directory"
    read -r uid gid mode < <("$TOOLCHAIN_STAT_BIN" -c '%u %g %a' -- "$directory") \
        || die "trusted toolchain directory metadata is unavailable: $directory"
    [[ "$uid:$gid" == 0:0 ]] \
        || die "trusted toolchain directory must be owned by root:root: $directory"
    (( (8#$mode & 8#022) == 0 )) \
        || die "trusted toolchain directory must not be group/other writable: $directory"
}

ensure_bootstrap_toolchain_root() {
    local command_path
    for command_path in "$TOOLCHAIN_ENV_BIN" "$TOOLCHAIN_CURL_BIN" \
        "$TOOLCHAIN_INSTALL_BIN" "$TOOLCHAIN_MV_BIN" "$TOOLCHAIN_READLINK_BIN" \
        "$TOOLCHAIN_SHA256_BIN" "$TOOLCHAIN_STAT_BIN" "$TOOLCHAIN_TAR_BIN"; do
        [[ -f "$command_path" && -x "$command_path" ]] ||
            die "required trusted toolchain command is unavailable: $command_path"
    done

    validate_bootstrap_trusted_directory /
    validate_bootstrap_trusted_directory /usr
    validate_bootstrap_trusted_directory /usr/bin

    if [[ ! -e /opt && ! -L /opt ]]; then
        "$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- /opt
    fi
    validate_bootstrap_trusted_directory /opt

    if [[ ! -e "$PREFIX" && ! -L "$PREFIX" ]]; then
        "$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$PREFIX"
    fi
    validate_bootstrap_trusted_directory "$PREFIX"

    if [[ ! -e "$TOOLCHAIN" && ! -L "$TOOLCHAIN" ]]; then
        "$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$TOOLCHAIN"
    fi
    validate_bootstrap_trusted_directory "$TOOLCHAIN"
}

toolchain_archive_sha256() {
    local product=$1 architecture=$2
    case "$product:$architecture" in
        go:amd64) printf '%s\n' "$GO_SHA256_AMD64" ;;
        go:arm64) printf '%s\n' "$GO_SHA256_ARM64" ;;
        node:amd64) printf '%s\n' "$NODE_SHA256_AMD64" ;;
        node:arm64) printf '%s\n' "$NODE_SHA256_ARM64" ;;
        *) die "unsupported toolchain checksum request: $product/$architecture" ;;
    esac
}

download_verified_toolchain_archive() {
    local url=$1 expected_sha256=$2 archive actual_sha256
    [[ "$expected_sha256" =~ ^[0-9a-f]{64}$ ]] || die "invalid pinned toolchain SHA-256"
    ensure_bootstrap_toolchain_root
    archive=$(mktemp "$TOOLCHAIN/.toolchain-download.XXXXXXXX") || die "cannot create private toolchain download"
    chmod 0600 "$archive"
    chown root:root "$archive"
    if ! run_external_clean "$TOOLCHAIN_CURL_BIN" --disable --proto '=https' --tlsv1.2 \
        --fail --location --retry 3 --show-error --silent --output "$archive" "$url"; then
        rm -f -- "$archive"
        die "toolchain archive could not be downloaded"
    fi
    read -r actual_sha256 _ <<<"$(run_external_clean "$TOOLCHAIN_SHA256_BIN" -- "$archive")" || {
        rm -f -- "$archive"
        die "toolchain archive could not be hashed"
    }
    if [[ "$actual_sha256" != "$expected_sha256" ]]; then
        rm -f -- "$archive"
        die "downloaded toolchain archive SHA-256 mismatch"
    fi
    printf '%s\n' "$archive"
}

# Toolchain download architecture, in Go/Node naming (amd64/arm64). uname -m
# instead of dpkg so this works on every distro.
# Araç zinciri indirme mimarisi, Go/Node adlandırmasıyla (amd64/arm64).
# dpkg yerine uname -m — böylece her dağıtımda çalışır.
dl_arch() {
    local machine
    machine=$(vendor_machine_architecture)
    case "$machine" in
        x86_64)  echo amd64 ;;
        aarch64) echo arm64 ;;
        *) die "desteklenmeyen mimari: $machine" ;;
    esac
}

go_toolchain_version() {
    local candidate=$1
    run_go_clean "$candidate" env GOVERSION 2>/dev/null
}

go_toolchain_is_exact() {
    local candidate=$1 expected_root=$2 version reported_root
    local canonical_candidate canonical_root
    canonical_candidate=$(readlink -e -- "$candidate") || return 1
    canonical_root=$(readlink -e -- "$expected_root") || return 1
    [[ "$canonical_candidate" == "$canonical_root/bin/go" ]] || return 1
    version=$(go_toolchain_version "$candidate") || return 1
    [[ "$version" == "go$GO_VERSION" ]] || return 1
    reported_root=$(run_go_clean "$candidate" env GOROOT 2>/dev/null) || return 1
    [[ "$reported_root" == "$canonical_root" ]]
}

rollback_bootstrap_go_publication() {
    local active_root=$1 retired_root=$2 failed_root=$3
    [[ -d "$retired_root" && ! -L "$retired_root" ]] || return 0
    if [[ -e "$active_root" || -L "$active_root" ]]; then
        if [[ -e "$failed_root" || -L "$failed_root" ]] ||
            ! "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$active_root" "$failed_root"; then
            c '1;31' "URGENT: interrupted Go candidate could not be isolated; previous Go remains at $retired_root" >&2
            return 1
        fi
    fi
    if [[ ! -e "$active_root" && ! -L "$active_root" ]] &&
        ! "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$retired_root" "$active_root"; then
        c '1;31' "URGENT: interrupted Go publication could not restore $retired_root" >&2
        return 1
    fi
}

publish_bootstrap_go_candidate() (
    local candidate_root=$1 retired_root=$2 failed_root=$3 rollback_armed=0
    finish_go_publication() {
        local rc=$?
        trap - EXIT
        if (( rollback_armed == 1 )); then
            rollback_bootstrap_go_publication "$TOOLCHAIN/go" "$retired_root" "$failed_root" || rc=1
        fi
        exit "$rc"
    }
    trap finish_go_publication EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    # Arm before the first rename. Cleanup inspects the filesystem, so a
    # signal in either rename window restores the old tree.
    rollback_armed=1
    "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$TOOLCHAIN/go" "$retired_root" || {
        rollback_armed=0
        die "cached Go toolchain could not be safely retired"
    }
    "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$candidate_root" "$TOOLCHAIN/go" ||
        die "Go publication failed; cleanup will restore the cached toolchain"
    seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"
    go_toolchain_is_exact "$TOOLCHAIN/go/bin/go" "$TOOLCHAIN/go" ||
        die "published Go toolchain is not exact go$GO_VERSION"
    rollback_armed=0
    trap - EXIT HUP INT TERM
)

bootstrap_go() {
    local arch expected archive staging path_go cached_go cached_version
    local cached_present=0 retired_go= failed_go=

    # Privileged builds use only the toolchain extracted from the archive whose
    # SHA-256 is pinned above. A same-version PATH binary is not equivalent:
    # its GOROOT/pkg/tool tree is outside this installer's sealed trust root.
    if path_go=$(command -v go 2>/dev/null); then
        warn "PATH Go ignored for privileged build: $path_go" \
            "Ayrıcalıklı derlemede PATH üzerindeki Go yok sayıldı: $path_go" >&2
    fi
    ensure_bootstrap_toolchain_root

    cached_go="$TOOLCHAIN/go/bin/go"
    if [[ -e "$TOOLCHAIN/go" || -L "$TOOLCHAIN/go" ]]; then
        validate_bootstrap_toolchain_tree "$TOOLCHAIN/go"
        [[ -x "$cached_go" ]] || die "cached Go toolchain has no executable go command"
        if go_toolchain_is_exact "$cached_go" "$TOOLCHAIN/go"; then
            printf '%s\n' "$cached_go"
            return
        fi
        cached_present=1
        cached_version=$(go_toolchain_version "$cached_go" || printf '%s' unreadable)
        warn "Cached Go will be retired: $cached_version (required go$GO_VERSION)" \
            "Önbellekteki Go kaldırılacak: $cached_version (gereken go$GO_VERSION)" >&2
    fi
    warn "Downloading Go $GO_VERSION…" "Go $GO_VERSION indiriliyor…" >&2
    arch=$(dl_arch)
    expected=$(toolchain_archive_sha256 go "$arch")
    archive=$(download_verified_toolchain_archive "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" "$expected")
    staging=$(mktemp -d "$TOOLCHAIN/.go-stage.XXXXXXXX") || {
        rm -f -- "$archive"
        die "cannot create Go staging directory"
    }
    chmod 0700 "$staging"
    chown root:root "$staging"
    if ! run_external_clean "$TOOLCHAIN_TAR_BIN" -xz --no-same-owner \
        -C "$staging" --file "$archive"; then
        rm -f -- "$archive"
        rm -rf -- "$staging"
        die "verified Go archive could not be extracted"
    fi
    rm -f -- "$archive"
    if [[ ! -d "$staging/go" || -L "$staging/go" ]]; then
        rm -rf -- "$staging"
        die "verified Go archive has an unexpected layout"
    fi
    if find "$staging" -mindepth 1 -maxdepth 1 ! -name go -print -quit | grep -q .; then
        rm -rf -- "$staging"
        die "verified Go archive contains unexpected top-level entries"
    fi
    seal_bootstrap_toolchain_tree "$staging/go"
    go_toolchain_is_exact "$staging/go/bin/go" "$staging/go" || {
        rm -rf -- "$staging"
        die "verified Go archive does not provide exact go$GO_VERSION"
    }

    # Never recursively delete a previously trusted compiler. Move it to a
    # root-owned retired name, publish the verified replacement atomically,
    # and roll back the move if publication fails. The operator may remove the
    # retired tree later after inspecting it.
    if (( cached_present )); then
        retired_go="$TOOLCHAIN/.go-retired.$(date -u +%Y%m%dT%H%M%SZ).$$"
        failed_go="$TOOLCHAIN/.go-failed.$(date -u +%Y%m%dT%H%M%SZ).$$"
        [[ ! -e "$retired_go" && ! -L "$retired_go" ]] \
            || die "refusing colliding retired Go path: $retired_go"
        [[ ! -e "$failed_go" && ! -L "$failed_go" ]] \
            || die "refusing colliding failed Go path: $failed_go"
        if ! publish_bootstrap_go_candidate "$staging/go" "$retired_go" "$failed_go"; then
            rm -rf -- "$staging"
            die "Go toolchain could not be published safely; inspect retained toolchain paths"
        fi
    else
        "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$staging/go" "$TOOLCHAIN/go" || {
            rm -rf -- "$staging"
            die "Go toolchain could not be published"
        }
    fi
    rmdir -- "$staging"
    seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"
    go_toolchain_is_exact "$TOOLCHAIN/go/bin/go" "$TOOLCHAIN/go" \
        || die "published Go toolchain is not exact go$GO_VERSION"
    [[ -z "$retired_go" ]] \
        || c '33' "    Previous Go retained for operator review: $retired_go" >&2
    printf '%s\n' "$TOOLCHAIN/go/bin/go"
}

bootstrap_node() {
    local arch archive expected node_archive_arch staging
    command -v npm >/dev/null && { echo "$(command -v node | xargs dirname)"; return; }
    ensure_bootstrap_toolchain_root
    if [ -x "$TOOLCHAIN/node/bin/npm" ]; then
        validate_bootstrap_toolchain_tree "$TOOLCHAIN/node"
        echo "$TOOLCHAIN/node/bin"
        return
    fi
    [[ ! -e "$TOOLCHAIN/node" && ! -L "$TOOLCHAIN/node" ]] || die "incomplete Node toolchain already exists"
    warn "Downloading Node $NODE_VERSION…" "Node $NODE_VERSION indiriliyor…" >&2
    arch=$(dl_arch)
    node_archive_arch=$arch
    [[ "$node_archive_arch" != amd64 ]] || node_archive_arch=x64
    expected=$(toolchain_archive_sha256 node "$arch")
    archive=$(download_verified_toolchain_archive "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-$node_archive_arch.tar.xz" "$expected")
    staging=$(mktemp -d "$TOOLCHAIN/.node-stage.XXXXXXXX") || {
        rm -f -- "$archive"
        die "cannot create Node staging directory"
    }
    chmod 0700 "$staging"
    chown root:root "$staging"
    if ! run_external_clean "$TOOLCHAIN_TAR_BIN" -xJ --no-same-owner \
        -C "$staging" --strip-components=1 --file "$archive"; then
        rm -f -- "$archive"
        rm -rf -- "$staging"
        die "verified Node archive could not be extracted"
    fi
    rm -f -- "$archive"
    chown -R -h root:root -- "$staging"
    chmod -R go-w -- "$staging"
    validate_bootstrap_toolchain_tree "$staging"
    mv -T --no-clobber -- "$staging" "$TOOLCHAIN/node" || {
        rm -rf -- "$staging"
        die "Node toolchain could not be published"
    }
    seal_bootstrap_toolchain_tree "$TOOLCHAIN/node"
    echo "$TOOLCHAIN/node/bin"
}

# Version stamped into BOTH binaries and the frontend, from one source: the
# git description of the commit being installed. Before this the footer showed
# a hand-typed "v0.1.0" that no build could change, so "which release is this
# server running?" had no honest answer.
# Sürüm, kurulan commit'in git tanımından TEK kaynaktan alınır ve HER İKİ
# binary ile ön yüze gömülür. Bundan önce footer, hiçbir derlemenin
# değiştiremediği elle yazılmış bir "v0.1.0" gösteriyordu; yani "bu sunucu
# hangi sürümü koşuyor?" sorusunun dürüst bir cevabı yoktu.
CP_VERSION=$(cd "$SRC" && git describe --tags --always --dirty 2>/dev/null || echo dev)
CP_COMMIT=$(cd "$SRC" && git rev-parse --short HEAD 2>/dev/null || echo unknown)
VER_FLAGS="-X main.buildVersion=$CP_VERSION -X main.buildCommit=$CP_COMMIT"

# A git checkout ALWAYS rebuilds. bin/ and web/dist are gitignored, so they
# survive `git pull` — and the old "skip the build if artifacts exist" test
# then installed the PREVIOUS build while update.sh printed "services
# restarted with the new build". A stale binary that reports the new release
# is worse than no version at all, especially when the new release is a
# security fix. Only a prebuilt release tarball (no .git) may skip.
# Bir git checkout HER ZAMAN yeniden derler. bin/ ve web/dist git'te olmadığı
# için `git pull`dan sağ çıkar — ve eski "ürünler varsa derlemeyi atla"
# denetimi, update.sh "servisler yeni yapıyla yeniden başladı" yazarken BİR
# ÖNCEKİ yapıyı kuruyordu. Yeni sürümü bildiren bayat bir binary, hiç sürüm
# olmamasından kötüdür; hele yeni sürüm bir güvenlik düzeltmesiyse. Yalnız
# önceden derlenmiş release tarball'ı (.git yok) atlayabilir.
if [ -d "$SRC/.git" ] || [ ! -x "$SRC/bin/panel" ] || [ ! -x "$SRC/bin/agent" ] || [ ! -f "$SRC/web/dist/index.html" ]; then
    step "Building from source (bin/panel, bin/agent, web/dist) — version $CP_VERSION" \
        "Kaynaktan derleme (bin/panel, bin/agent, web/dist) — sürüm $CP_VERSION"
    GO_BIN=$(bootstrap_go)
    NODE_BIN=$(bootstrap_node)
    ( cd "$SRC" && run_go_clean "$GO_BIN" build -trimpath -buildvcs=false -ldflags "-s -w $VER_FLAGS" -o bin/panel ./cmd/panel ) || die "Panel build failed" "Panel derlenemedi"
    ( cd "$SRC" && run_go_clean "$GO_BIN" build -trimpath -buildvcs=false -ldflags "-s -w $VER_FLAGS" -o bin/agent ./cmd/agent ) || die "Agent build failed" "Agent derlenemedi"
    ( cd "$SRC/web" && run_node_clean "$NODE_BIN" "$NODE_BIN/npm" ci --no-audit --no-fund >/dev/null 2>&1 ) || die "npm installation failed" "npm kurulumu başarısız"
    ( cd "$SRC/web" && run_node_clean "$NODE_BIN" "$NODE_BIN/npm" run build >/dev/null ) || die "Frontend build failed" "Frontend derlenemedi"
    ok "built ($CP_VERSION · $CP_COMMIT)" "derlendi ($CP_VERSION · $CP_COMMIT)"
else
    ok "Using the prebuilt release (bin/ + web/dist)" \
        "Önceden derlenmiş sürüm kullanılıyor (bin/ + web/dist)"
fi

# 4. Install files -----------------------------------------------------------
step "Installing files under $PREFIX" "Dosyalar $PREFIX altına kuruluyor"
install -d -m 0755 "$PREFIX/bin" "$PREFIX/web" "$PREFIX/runtimes"
install -m 0755 "$SRC/bin/panel" "$PREFIX/bin/panel"
install -m 0755 "$SRC/bin/agent" "$PREFIX/bin/agent"

# Replace the exact fixed web root, including hidden entries and empty stale
# directories. Canonical root-owned boundaries are proven before -delete runs.
# Gizli girdiler ve boş bayat dizinler dahil tam sabit web root'unu değiştir.
# -delete çalışmadan önce kanonik root-sahipli sınırlar kanıtlanır.
installed_web_root="$PREFIX/web"
[[ "$PREFIX" == /opt/celikpanel && "$installed_web_root" == /opt/celikpanel/web ]] \
    || die "web kurulum root sınırı beklenmedik"
for install_root in /opt "$PREFIX" "$installed_web_root"; do
    [[ -d "$install_root" && ! -L "$install_root" ]] \
        || die "güvensiz web kurulum sınırı: $install_root"
    install_canonical=$(readlink -e -- "$install_root") \
        || die "web kurulum sınırı çözümlenemedi: $install_root"
    [[ "$install_canonical" == "$install_root" ]] \
        || die "web kurulum sınırı alias içeriyor: $install_root"
    read -r install_owner install_group install_mode < <(stat -Lc '%u %g %a' -- "$install_root") \
        || die "web kurulum sınırı incelenemedi: $install_root"
    install_permissions=$((8#$install_mode))
    [[ "$install_owner" == 0 && "$install_group" == 0 ]] && \
        (( (install_permissions & 0022) == 0 )) \
        || die "web kurulum sınırı root:root ve group/other yazılamaz olmalı: $install_root"
done
[[ -d "$SRC/web/dist" && ! -L "$SRC/web/dist" ]] \
    || die "web kaynak ağacı eksik veya güvensiz"
if find "$SRC/web/dist" -type l -print -quit | grep -q .; then
    die "web kaynak ağacı sembolik bağlantı içeriyor"
fi
if find "$SRC/web/dist" ! -type d ! -type f -print -quit | grep -q .; then
    die "web kaynak ağacı özel dosya sistemi nesnesi içeriyor"
fi
find "$installed_web_root" -xdev -mindepth 1 -depth -delete \
    || die "eski web ağacı tam temizlenemedi"
if find "$installed_web_root" -mindepth 1 -print -quit | grep -q .; then
    die "eski web ağacı temizlendikten sonra girdi kaldı"
fi
cp -a "$SRC/web/dist/." "$installed_web_root/"
# Static assets are public product bytes, not secrets. Do not preserve a
# caller's restrictive umask (for example 0077) through the build directory
# and cp -a: the unprivileged panel process must traverse the complete tree
# and read every asset. Normalize the already proven symlink-free tree and
# verify it before any service is started.
chown -R root:root -- "$installed_web_root"
find "$installed_web_root" -xdev -type d -exec chmod 0755 -- {} + \
    || die "installed web directory permissions could not be normalized"
find "$installed_web_root" -xdev -type f -exec chmod 0644 -- {} + \
    || die "installed web file permissions could not be normalized"
if find "$installed_web_root" -xdev -type d ! -perm 0755 -print -quit | grep -q .; then
    die "installed web directory permissions could not be verified"
fi
if find "$installed_web_root" -xdev -type f ! -perm 0644 -print -quit | grep -q .; then
    die "installed web file permissions could not be verified"
fi
[[ -f "$installed_web_root/index.html" && ! -L "$installed_web_root/index.html" ]] \
    || die "kurulu web index ürünü eksik veya güvensiz"
# Runtimes dir is where the agent installs Node versions; group-owned so the
# root agent writes and the panel can stat.
# Runtimes dizini agent'ın Node sürümlerini kurduğu yerdir; grup-sahipli.
chown -R root:"$SVC_GROUP" "$PREFIX/runtimes"
chmod 0775 "$PREFIX/runtimes"
ok "installed" "kuruldu"

# 5. Data directory (SQLite lives here; StateDirectory also ensures it) ------
step "Data directory $DATA_DIR" "Veri dizini $DATA_DIR"
install -d -m 0750 -o "$SVC_USER" -g "$SVC_GROUP" "$DATA_DIR"
# Privileged imports must not live below the panel-owned data directory. The
# root agent accepts only owner-only regular files from this root; the
# unprivileged panel merely forwards the operator-selected path.
install -d -m 0700 -o root -g root "$IMPORT_DIR"
ok "ready" "hazır"

# 6. systemd units -----------------------------------------------------------
step "systemd services" "systemd servisleri"
install -m 0644 "$SRC/deploy/systemd/celikpanel-agent.service" /etc/systemd/system/
install -m 0644 "$SRC/deploy/systemd/celikpanel-firewall-restore.service" /etc/systemd/system/
install -m 0644 "$SRC/deploy/systemd/celikpanel-panel.service" /etc/systemd/system/
provision_signed_update_lock
install_reviewed_release_updater
# Operator choices live in root-only /etc/celikpanel/panel.env. Reinstall and
# update always replace the vendor unit, never that durable configuration.
# Operatör seçimleri root-only /etc/celikpanel/panel.env içindedir. Yeniden
# kurulum ve güncelleme üretici unitini yeniler; kalıcı ayara dokunmaz.
if [[ "$VALIDATED_PANEL_HTTPS" == 0 ]]; then
    warn "R&D mode: demo accounts are enabled and cookies work over plain HTTP — do not expose this server to the internet" \
        "AR-GE modu: demo hesaplar açık, çerezler düz HTTP'de çalışır — internete açmayın"
fi
restore_celikpanel_selinux_labels
"$SYSTEMCTL_BIN" daemon-reload
ok "installed" "kuruldu"

# Apply-only ends after immutable layout and ledger metadata are verified. It
# never initializes state, reconciles firewall, enables/starts services, waits
# for sockets, creates admins, or runs panel migrations.
# Apply-only değişmez yerleşim ve ledger metadata doğrulamasından sonra biter.
# State başlatmaz, firewall uzlaştırmaz, servis açıp başlatmaz, socket beklemez,
# admin oluşturmaz veya panel migration çalıştırmaz.
if [[ $APPLY_ONLY -eq 1 ]]; then
    [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] || die "apply-only durable agent ledger is missing"
    read -r ledger_owner ledger_group ledger_mode < <(stat -Lc '%u %g %a' -- "$AGENT_LEDGER") || die "apply-only cannot inspect agent ledger"
    [[ "$ledger_owner" == 0 && "$ledger_group" == "$SVC_GROUP_ID" && "$ledger_mode" == 600 ]] || die "apply-only agent ledger metadata mismatch"
    sync -f -- "$PREFIX/bin/panel" "$PREFIX/bin/agent" "$PREFIX/bin" "$PREFIX/web" \
        "$PANEL_ENV" "$CONF_DIR" /etc/systemd/system \
        || die "apply-only installed layout could not be made durable"
    ok "apply-only layout completed; services were left stopped" \
        "apply-only yerleşim tamamlandı; servisler kapalı bırakıldı"
    exit 0
fi

# 7. Start the agent (generates the shared token on first run) ---------------
# restart, not enable --now: an upgrade must actually load the new binary;
# --now is a no-op when the service is already running.
# enable --now değil restart: yükseltme yeni binary'yi gerçekten yüklemeli;
# servis zaten çalışıyorsa --now hiçbir şey yapmaz.
step "Starting the agent" "Agent başlatılıyor"
if [[ $initialize_ledger -eq 1 ]]; then
    [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || die "servis işlem ledger başlatmadan önce mevcut"
    mutation_lock_dir=$(dirname "$MUTATION_LOCK")
    if [[ -e "$mutation_lock_dir" || -L "$mutation_lock_dir" ]]; then
        [[ -d "$mutation_lock_dir" && ! -L "$mutation_lock_dir" ]] \
            || die "güvensiz servis işlem kilit dizini: $mutation_lock_dir"
        read -r lock_owner lock_mode < <(stat -Lc '%u %a' -- "$mutation_lock_dir") \
            || die "servis işlem kilit dizini incelenemedi"
        lock_permissions=$((8#$lock_mode))
        [[ "$lock_owner" == 0 ]] && (( (lock_permissions & 0022) == 0 )) \
            || die "servis işlem kilit dizini root sahipli ve group/other yazılamaz olmalı"
    fi
    install -d -m 0700 -o root -g "$SVC_GROUP" -- "$AGENT_STATE_DIR"
    install -d -m 0750 -o root -g "$SVC_GROUP" -- "$mutation_lock_dir"
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \
    CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFIX/bin/agent" --initialize-service-mutation-ledger \
        || die "servis işlem ledger başlatılamadı"
fi
[[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
    || die "servis işlem ledger eksik veya güvensiz"
read -r ledger_owner ledger_group ledger_mode < <(stat -Lc '%u %g %a' -- "$AGENT_LEDGER") \
    || die "servis işlem ledger incelenemedi"
[[ "$ledger_owner" == 0 && "$ledger_group" == "$SVC_GROUP_ID" && "$ledger_mode" == 600 ]] \
    || die "servis işlem ledger root:$SVC_GROUP mode 0600 olmalı"
# A fresh install only lays down the restore unit. Existing persistence is
# re-enabled during upgrade, but no activation link is created before the
# operator explicitly chooses Save for reboot in the panel.
# Temiz kurulum yalnız restore unitini yerleştirir. Mevcut kalıcılık yükseltmede
# yeniden etkinleştirilir; operatör panelde açıkça Save for reboot seçmeden
# hiçbir etkinleştirme bağlantısı oluşturulmaz.
/bin/bash "$SRC/deploy/systemd/enable-firewall-restore-if-saved.sh" "$CONF_DIR/firewall.nft" || \
    die "firewall restore unit could not be reconciled"
"$SYSTEMCTL_BIN" enable celikpanel-agent.service >/dev/null 2>&1 || true
"$SYSTEMCTL_BIN" restart celikpanel-agent.service || \
    die "The agent could not be restarted — inspect 'journalctl -u celikpanel-agent'" \
        "Agent yeniden başlatılamadı — 'journalctl -u celikpanel-agent' inceleyin"
"$SYSTEMCTL_BIN" is-active --quiet celikpanel-agent.service || \
    die "The agent is not active — inspect 'journalctl -u celikpanel-agent'" \
        "Agent aktif değil — 'journalctl -u celikpanel-agent' inceleyin"
for _ in $(seq 1 20); do
    [ -S /run/celikpanel/agent.sock ] && break
    sleep 0.3
done
[ -S /run/celikpanel/agent.sock ] || die \
    "The agent socket was not created — inspect 'journalctl -u celikpanel-agent'" \
    "Agent socket oluşmadı — 'journalctl -u celikpanel-agent' inceleyin"
ok "agent is running" "agent çalışıyor"

# 8. First administrator -----------------------------------------------------
ensure_first_administrator

# 9. Start the panel ---------------------------------------------------------
step "Starting the panel" "Panel başlatılıyor"
"$SYSTEMCTL_BIN" enable celikpanel-panel.service >/dev/null 2>&1 || true
"$SYSTEMCTL_BIN" restart celikpanel-panel.service || \
    die "The panel could not be restarted — inspect 'journalctl -u celikpanel-panel'" \
        "Panel yeniden başlatılamadı — 'journalctl -u celikpanel-panel' inceleyin"
sleep 1
"$SYSTEMCTL_BIN" is-active --quiet celikpanel-panel.service || \
    die "The panel did not start — inspect 'journalctl -u celikpanel-panel'" \
        "Panel başlamadı — 'journalctl -u celikpanel-panel' inceleyin"
ok "panel is running" "panel çalışıyor"

step "Protecting the initial panel certificate" "Ilk panel sertifikasi korumaya aliniyor"
"$SYSTEMCTL_BIN" stop celikpanel-panel.service || \
    die "The panel could not be stopped for certificate protection" \
        "Sertifika korumasi icin panel durdurulamadi"
panel_tls_normalize_legacy_self_signed \
    "$PANEL_TLS_DIR" "$(id -u "$SVC_USER")" "$(getent group "$SVC_GROUP" | cut -d: -f3)" || \
    die "The initial panel certificate metadata could not be protected" \
        "Ilk panel sertifikasi metaverisi korunamadi"
"$SYSTEMCTL_BIN" restart celikpanel-panel.service || \
    die "The panel could not be restarted after certificate protection" \
        "Sertifika korumasindan sonra panel yeniden baslatilamadi"
sleep 1
"$SYSTEMCTL_BIN" is-active --quiet celikpanel-panel.service || \
    die "The panel did not start after certificate protection" \
        "Sertifika korumasindan sonra panel baslamadi"
ok "initial panel certificate is protected" "ilk panel sertifikasi korundu"

# Record completion only after both services and the administrator path have
# succeeded. The public bootstrapper uses this root-only marker to distinguish
# a finished installation from a failed first-admin attempt.
install -m 0600 -o root -g root /dev/null "$INSTALL_COMPLETE"
sync -f "$INSTALL_COMPLETE" "$CONF_DIR"

# 10. Done -------------------------------------------------------------------
IP=""
if command -v hostname >/dev/null 2>&1; then
    IP="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
fi
PORT="${VALIDATED_PANEL_LISTEN##*:}"
PANEL_SCHEME=https
[[ "$VALIDATED_PANEL_HTTPS" == 1 ]] || PANEL_SCHEME=http
echo
c '1;32' "CelikPanel was installed successfully. / CelikPanel başarıyla kuruldu."
echo "    Panel:  ${PANEL_SCHEME}://${IP:-SUNUCU_IP}:${PORT}"
echo "    Services / Servisler: systemctl status celikpanel-agent celikpanel-panel"
echo "    Logs / Günlükler: journalctl -u celikpanel-panel -f"
